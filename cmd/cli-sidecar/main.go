package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Bhattisahb121/cli-sidecar/internal/adapter"
	"github.com/Bhattisahb121/cli-sidecar/internal/config"
	"github.com/Bhattisahb121/cli-sidecar/internal/container"
	"github.com/Bhattisahb121/cli-sidecar/internal/coordinator"
	"github.com/Bhattisahb121/cli-sidecar/internal/server"
	"github.com/Bhattisahb121/cli-sidecar/internal/session"
)

const version = "0.2.0"

func main() {
	if len(os.Args) > 1 {
		switch strings.ToLower(os.Args[1]) {
		case "init":
			runInit()
			return
		case "coordinator":
			runCoordinator()
			return
		case "help", "--help", "-h":
			printHelp()
			return
		case "version", "--version", "-v":
			fmt.Printf("cli-sidecar v%s\n", version)
			return
		}
	}

	// Default: standalone mode (no Docker, direct CLI execution)
	runStandalone()
}

func runStandalone() {
	cfgPath := config.DefaultConfigPath()
	if envPath := os.Getenv("CLI_SIDECAR_CONFIG"); envPath != "" {
		cfgPath = envPath
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %s", err)
	}

	registry := adapter.NewRegistry(cfg.Tools)

	log.Printf("[standalone] Registered tools: %v", registry.List())
	log.Printf("[standalone] Available tools: %v", registry.ListAvailable())

	sessionMgr := session.NewManager(registry)
	srv := server.New(registry, sessionMgr, cfg.Host, cfg.Port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("Server error: %s", err)
		}
	}()

	sig := <-sigCh
	log.Printf("Received %s, shutting down...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func runCoordinator() {
	cfgPath := config.DefaultConfigPath()
	if envPath := os.Getenv("CLI_SIDECAR_CONFIG"); envPath != "" {
		cfgPath = envPath
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %s", err)
	}

	// Check Docker availability
	if err := container.InspectDocker(); err != nil {
		log.Fatalf("Docker is required for coordinator mode: %s", err)
	}

	network := cfg.Network
	if network == "" {
		network = "cli-sidecar-net"
	}

	shimImage := cfg.ShimImage
	if shimImage == "" {
		shimImage = "cli-sidecar-shim:latest"
	}

	containerMgr := container.NewManager(network, shimImage)
	if err := containerMgr.EnsureNetwork(); err != nil {
		log.Fatalf("Failed to setup Docker network: %s", err)
	}

	coord := coordinator.New(containerMgr)

	// Build HTTP server with both standalone and coordinator routes
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","mode":"coordinator"}`))
	})

	// Coordinator routes
	coord.RegisterRoutes(mux)

	// Also register standalone routes if tools are configured
	if len(cfg.Tools) > 0 {
		registry := adapter.NewRegistry(cfg.Tools)
		sessionMgr := session.NewManager(registry)
		toolsHandler := server.HandleListToolsFunc(registry)
		runHandler := server.HandleRunFunc(sessionMgr)
		streamHandler := server.HandleStreamFunc(sessionMgr)
		sessionsHandler := server.HandleSessionsFunc(sessionMgr)
		sessionByIDHandler := server.HandleSessionByIDFunc(sessionMgr)
		mux.HandleFunc("/api/tools", toolsHandler)
		mux.HandleFunc("/api/run", runHandler)
		mux.HandleFunc("/api/stream", streamHandler)
		mux.HandleFunc("/api/sessions", sessionsHandler)
		mux.HandleFunc("/api/sessions/", sessionByIDHandler)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      server.CORSMiddleware(server.LogMiddleware(mux)),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("[coordinator] Starting on %s (network: %s, image: %s)", addr, network, shimImage)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %s", err)
		}
	}()

	sig := <-sigCh
	log.Printf("Received %s, cleaning up...", sig)

	// Cleanup containers
	containerMgr.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func runInit() {
	cfgPath := config.DefaultConfigPath()
	if envPath := os.Getenv("CLI_SIDECAR_CONFIG"); envPath != "" {
		cfgPath = envPath
	}

	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("Config file already exists: %s\n", cfgPath)
		fmt.Println("Edit it to update your settings.")
		return
	}

	cfg := config.DefaultConfig()
	if err := config.Save(cfgPath, cfg); err != nil {
		log.Fatalf("Failed to create config: %s", err)
	}

	fmt.Printf("Config file created: %s\n\n", cfgPath)
	fmt.Println("Default tools configured: claude, codex, gemini")
	fmt.Println("Edit the config to add custom tools or change settings.")
	fmt.Printf("\nServer will listen on %s:%d\n", cfg.Host, cfg.Port)
}

func printHelp() {
	fmt.Printf(`cli-sidecar v%s — Web-to-CLI bridge for AI coding assistants

Usage:
  cli-sidecar               Start in standalone mode (direct CLI execution)
  cli-sidecar coordinator   Start in coordinator mode (Docker containers + shims)
  cli-sidecar init          Create a config file
  cli-sidecar help          Show this help
  cli-sidecar version       Show version

Modes:
  standalone (default)      Runs CLI tools directly as subprocesses
  coordinator               Spawns Alpine containers with shim processes;
                            each container runs one CLI tool persistently

Coordinator API:
  POST /api/prompt          Send prompt to a container's shim
    {"tool":"claude","account":"user1","prompt":"explain goroutines"}

  POST /api/containers      Spawn a new container
    {"tool":"claude","account":"user1","cli_command":"claude","cli_args":"-i"}

  GET    /api/containers              List all containers
  GET    /api/containers/:name        Get container info
  GET    /api/containers/:name/health Check shim health
  GET    /api/containers/:name/logs   Get container logs
  POST   /api/containers/:name/stop   Stop a container
  DELETE /api/containers/:name        Remove a container

Container Naming: {tool}-{account}-{seq} (e.g., claude-user1-001)

Standalone API (also available in coordinator mode):
  GET  /api/tools             List registered tools
  POST /api/run               Run prompt synchronously
  POST /api/stream            Run prompt with SSE streaming
  GET  /api/sessions          List sessions
`, version)
}

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Bhattisahb121/cli-sidecar/internal/adapter"
	"github.com/Bhattisahb121/cli-sidecar/internal/config"
	"github.com/Bhattisahb121/cli-sidecar/internal/server"
	"github.com/Bhattisahb121/cli-sidecar/internal/session"
)

func main() {
	if len(os.Args) > 1 {
		switch strings.ToLower(os.Args[1]) {
		case "init":
			runInit()
			return
		case "help", "--help", "-h":
			printHelp()
			return
		case "version", "--version", "-v":
			fmt.Println("cli-sidecar v0.1.0")
			return
		}
	}

	cfgPath := config.DefaultConfigPath()
	if envPath := os.Getenv("CLI_SIDECAR_CONFIG"); envPath != "" {
		cfgPath = envPath
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %s", err)
	}

	registry := adapter.NewRegistry(cfg.Tools)

	log.Printf("Registered tools: %v", registry.List())
	log.Printf("Available tools: %v", registry.ListAvailable())

	sessionMgr := session.NewManager(registry)
	srv := server.New(registry, sessionMgr, cfg.Host, cfg.Port)

	// Graceful shutdown
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
	fmt.Println(`cli-sidecar — Web-to-CLI bridge for AI coding assistants

Usage:
  cli-sidecar          Start the sidecar server
  cli-sidecar init     Create a config file
  cli-sidecar help     Show this help
  cli-sidecar version  Show version

Environment:
  CLI_SIDECAR_CONFIG   Path to config file (default: ~/.config/cli-sidecar/config.json)

API Endpoints:
  GET  /health                    Health check
  GET  /api/tools                 List registered tools
  POST /api/run                   Run prompt synchronously (blocks until done)
  POST /api/stream                Run prompt with SSE streaming
  GET  /api/sessions              List all sessions
  GET  /api/sessions/:id          Get session by ID
  POST /api/sessions/:id/cancel   Cancel a running session
  DELETE /api/sessions/:id        Delete a session

Request Body (for /api/run and /api/stream):
  {
    "tool": "claude",
    "prompt": "explain this code"
  }

Supported Tools (default):
  claude    Claude CLI (claude -p "prompt")
  codex     Codex CLI (codex exec "prompt")
  gemini    Gemini CLI (gemini -p "prompt")

Custom Tools:
  Add any CLI tool to config.json with:
  {
    "name": "my-tool",
    "command": "/path/to/tool",
    "args": ["--flag"],
    "enabled": true
  }

Examples:
  # Start the server
  cli-sidecar

  # Run a prompt synchronously
  curl -X POST http://localhost:8830/api/run \
    -H "Content-Type: application/json" \
    -d '{"tool": "claude", "prompt": "explain goroutines"}'

  # Stream a response (SSE)
  curl -N -X POST http://localhost:8830/api/stream \
    -H "Content-Type: application/json" \
    -d '{"tool": "claude", "prompt": "write a hello world in Go"}'`)
}

# cli-sidecar

CLI 기반 AI 코딩 어시스턴트(Claude CLI, Codex CLI, Gemini CLI 등)를 웹 API로 조종하는 사이드카 프로그램.

웹 요청을 받아서 CLI 도구에 텍스트를 입력하고, 결과를 다시 웹 응답으로 돌려줍니다.

## 아키텍처

두 가지 실행 모드를 지원합니다:

### Standalone 모드 (기본)

```
웹 클라이언트 → HTTP 요청 → cli-sidecar → CLI 도구 실행 → 응답 캡처 → HTTP 응답
```

CLI 도구를 직접 subprocess로 실행합니다. Docker 없이 간단하게 사용할 때.

### Coordinator 모드 (Docker 컨테이너 기반)

```
웹 클라이언트
    │
    ▼
Coordinator (메인 프로세스, :8830)
    ├── 라우팅: tool + account 기반
    │
    ├── 컨테이너: claude-user1-001 (Alpine)
    │     └── Shim (:8831) ←→ Claude CLI (상주, PTY)
    │
    ├── 컨테이너: claude-user2-002 (Alpine)
    │     └── Shim (:8831) ←→ Claude CLI (다른 계정)
    │
    ├── 컨테이너: codex-team1-003 (Alpine)
    │     └── Shim (:8831) ←→ Codex CLI (상주, PTY)
    │
    └── ... (같은 Docker 네트워크: cli-sidecar-net)
```

- **Coordinator**: 웹 요청을 받아 적절한 컨테이너의 Shim으로 라우팅
- **Shim**: 각 컨테이너 안에서 CLI 도구를 PTY로 띄워놓고 지속적으로 요청 대행
- **컨테이너 네이밍**: `{tool}-{account}-{seq}` (예: `claude-user1-001`)
- **네트워크**: 모든 컨테이너가 같은 Docker 네트워크에 연결
- CLI를 매번 재실행하지 않고 상주시켜 부하 최소화

**핵심 기능:**
- 여러 LLM을 동시 실행 + 개별 LLM이 다른 계정 사용 가능
- ANSI → Markdown 역변환 (PTY 출력을 깔끔한 텍스트로 변환)
- 동기 실행과 SSE 스트리밍 모두 지원

## 설치

```bash
go install github.com/Bhattisahb121/cli-sidecar/cmd/cli-sidecar@latest
```

또는 소스에서 빌드:

```bash
git clone https://github.com/Bhattisahb121/cli-sidecar.git
cd cli-sidecar
make build-all
```

## 빠른 시작

### Standalone 모드

```bash
# 설정 파일 생성
cli-sidecar init

# 서버 시작
cli-sidecar

# 다른 터미널에서 테스트
curl -X POST http://localhost:8830/api/run \
  -H "Content-Type: application/json" \
  -d '{"tool": "claude", "prompt": "explain goroutines in Go"}'
```

### Coordinator 모드 (Docker)

```bash
# Shim 이미지 빌드
make docker-shim

# Coordinator 시작
cli-sidecar coordinator

# Claude 컨테이너 생성 (user1 계정)
curl -X POST http://localhost:8830/api/containers \
  -H "Content-Type: application/json" \
  -d '{
    "tool": "claude",
    "account": "user1",
    "cli_command": "claude",
    "cli_args": "-i",
    "output_mode": "markdown"
  }'

# Codex 컨테이너 생성 (다른 계정)
curl -X POST http://localhost:8830/api/containers \
  -H "Content-Type: application/json" \
  -d '{
    "tool": "codex",
    "account": "team1",
    "cli_command": "codex",
    "output_mode": "plain"
  }'

# 프롬프트 전송 (자동으로 해당 컨테이너의 Shim으로 라우팅)
curl -X POST http://localhost:8830/api/prompt \
  -H "Content-Type: application/json" \
  -d '{"tool": "claude", "account": "user1", "prompt": "explain goroutines"}'
```

## API

### `GET /health`
헬스 체크

### `GET /api/tools`
등록된 도구 목록과 사용 가능 여부 확인

```json
[
  {"name": "claude", "available": true},
  {"name": "codex", "available": true},
  {"name": "gemini", "available": false}
]
```

### `POST /api/run`
프롬프트를 동기적으로 실행 (완료될 때까지 대기)

```bash
curl -X POST http://localhost:8830/api/run \
  -H "Content-Type: application/json" \
  -d '{"tool": "claude", "prompt": "hello world를 Go로 작성해줘"}'
```

응답:
```json
{
  "session_id": "sess_1234_1",
  "tool": "claude",
  "output": "package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"Hello, World!\")\n}"
}
```

### `POST /api/stream`
SSE 스트리밍으로 실시간 응답 수신

```bash
curl -N -X POST http://localhost:8830/api/stream \
  -H "Content-Type: application/json" \
  -d '{"tool": "claude", "prompt": "explain concurrency"}'
```

이벤트 형식:
```
event: session
data: sess_1234_1

event: token
data: {"text": "Concurrency is..."}

event: token
data: {"text": " the ability to..."}

event: done
data: complete
```

### `GET /api/sessions`
모든 세션 목록

### `GET /api/sessions/:id`
특정 세션 조회

### `POST /api/sessions/:id/cancel`
실행 중인 세션 취소

### `DELETE /api/sessions/:id`
세션 삭제

### Coordinator API (coordinator 모드 전용)

#### `POST /api/containers`
새 컨테이너 생성

```json
{
  "tool": "claude",
  "account": "user1",
  "cli_command": "claude",
  "cli_args": "-i",
  "output_mode": "markdown",
  "env": {"ANTHROPIC_API_KEY": "sk-..."}
}
```

#### `POST /api/prompt`
컨테이너의 Shim에 프롬프트 전송 (tool + account로 자동 라우팅)

```json
{"tool": "claude", "account": "user1", "prompt": "explain goroutines"}
```

#### `GET /api/containers`
모든 컨테이너 목록

#### `GET /api/containers/:name`
컨테이너 정보 조회

#### `GET /api/containers/:name/health`
Shim 헬스 체크

#### `GET /api/containers/:name/logs`
컨테이너 로그 조회

#### `POST /api/containers/:name/stop`
컨테이너 중지

#### `DELETE /api/containers/:name`
컨테이너 제거

## 설정

설정 파일: `~/.config/cli-sidecar/config.json`

```json
{
  "host": "127.0.0.1",
  "port": 8830,
  "tools": [
    {
      "name": "claude",
      "command": "claude",
      "args": ["-p"],
      "enabled": true,
      "use_pty": true,
      "output_mode": "markdown"
    },
    {
      "name": "codex",
      "command": "codex",
      "args": ["exec"],
      "enabled": true,
      "output_mode": "plain"
    },
    {
      "name": "gemini",
      "command": "gemini",
      "args": ["-p"],
      "enabled": true,
      "output_mode": "dumb"
    }
  ],
  "network": "cli-sidecar-net",
  "shim_image": "cli-sidecar-shim:latest"
}
```

`network`와 `shim_image`는 coordinator 모드에서만 사용됩니다.

### 출력 모드 (Output Mode)

CLI 도구의 출력을 처리하는 4가지 모드:

| 모드 | 설명 | 용도 |
|------|------|------|
| `raw` | 출력을 그대로 반환 (ANSI 코드 포함) | 터미널 클라이언트가 직접 렌더링할 때 |
| `plain` | ANSI 코드를 모두 제거, 순수 텍스트 반환 | 단순 텍스트만 필요할 때 |
| `markdown` | ANSI 스타일을 Markdown으로 역변환 | **CLI 도구의 ANSI 출력을 Markdown으로 복원할 때** |
| `dumb` | `TERM=dumb` + `NO_COLOR=1` 설정하여 CLI가 ANSI 없이 출력 | CLI가 plain text 모드를 지원할 때 |

### PTY 모드

`"use_pty": true`를 설정하면 CLI 도구를 PTY(의사 터미널)에서 실행합니다.
일부 도구는 PTY가 있어야 정상 동작하거나 색상 출력을 생성합니다.
PTY 모드에서는 `output_mode: "markdown"`과 함께 사용하면 ANSI 출력이 자동으로 Markdown으로 변환됩니다.

### 커스텀 도구 추가

어떤 CLI 도구든 추가할 수 있습니다. 프롬프트가 마지막 인자로 전달됩니다:

```json
{
  "name": "my-ai",
  "command": "/usr/local/bin/my-ai-tool",
  "args": ["--mode", "chat", "--query"],
  "env": {
    "MY_AI_KEY": "..."
  },
  "enabled": true,
  "use_pty": false,
  "output_mode": "plain"
}
```

실행 시: `/usr/local/bin/my-ai-tool --mode chat --query "사용자 프롬프트"`

## 기본 도구별 CLI 명령

| 도구 | 실행 명령 | 비고 |
|------|-----------|------|
| Claude | `claude -p "prompt"` | 비대화형 모드, stdout 출력 |
| Codex | `codex exec "prompt"` | 비대화형, stderr에 진행 상황, stdout에 최종 결과 |
| Gemini | `gemini -p "prompt"` | 비대화형 모드, stdout 출력 |

## 환경 변수

| 변수 | 설명 |
|------|------|
| `CLI_SIDECAR_CONFIG` | 설정 파일 경로 (기본: `~/.config/cli-sidecar/config.json`) |

## 요구 사항

- Go 1.23+
- **Standalone 모드**: CLI 도구가 PATH에 설치되어 있어야 함
- **Coordinator 모드**: Docker 필요, CLI 도구는 컨테이너 이미지에 포함

## 라이선스

MIT

# cli-sidecar

CLI 기반 AI 코딩 어시스턴트(Claude CLI, Codex CLI, Gemini CLI 등)를 웹 API로 조종하는 사이드카 프로그램.

웹 요청을 받아서 CLI 도구에 텍스트를 입력하고, 결과를 다시 웹 응답으로 돌려줍니다.

## 동작 방식

```
웹 클라이언트 → HTTP 요청 → cli-sidecar → CLI 도구 실행 → 응답 캡처 → HTTP 응답
```

- 각 CLI 도구를 subprocess 또는 PTY로 실행하여 stdin/stdout을 제어
- 동기 실행(`/api/run`)과 SSE 스트리밍(`/api/stream`) 모두 지원
- 여러 세션을 동시에 관리 가능
- **ANSI → Markdown 역변환**: PTY에서 캡처한 ANSI 출력을 Markdown으로 자동 변환

## 설치

```bash
go install github.com/Bhattisahb121/cli-sidecar/cmd/cli-sidecar@latest
```

또는 소스에서 빌드:

```bash
git clone https://github.com/Bhattisahb121/cli-sidecar.git
cd cli-sidecar
make build
```

## 빠른 시작

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
  ]
}
```

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
- 사용하려는 CLI 도구가 PATH에 설치되어 있어야 함

## 라이선스

MIT

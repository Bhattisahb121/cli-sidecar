# cli-sidecar

CLI 기반 AI 코딩 어시스턴트(Claude CLI, Codex CLI, Gemini CLI 등)를 웹 API로 조종하는 사이드카 프로그램.

웹 요청을 받아서 CLI 도구에 텍스트를 입력하고, 결과를 다시 웹 응답으로 돌려줍니다.

## 동작 방식

```
웹 클라이언트 → HTTP 요청 → cli-sidecar → CLI 도구 실행 → 응답 캡처 → HTTP 응답
```

- 각 CLI 도구를 subprocess로 실행하여 stdin/stdout을 제어
- 동기 실행(`/api/run`)과 SSE 스트리밍(`/api/stream`) 모두 지원
- 여러 세션을 동시에 관리 가능

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
      "enabled": true
    },
    {
      "name": "codex",
      "command": "codex",
      "args": ["exec"],
      "enabled": true
    },
    {
      "name": "gemini",
      "command": "gemini",
      "args": ["-p"],
      "enabled": true
    }
  ]
}
```

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
  "enabled": true
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

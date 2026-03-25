# Contributing Guide

Language: [中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

Thanks for your interest in contributing to DS2API!

## Development Setup

### Prerequisites

- Go 1.24+
- Node.js 20+ (for WebUI development)
- npm (bundled with Node.js)

### Backend Development

```bash
# 1. Clone
git clone https://github.com/CJackHwang/ds2api.git
cd ds2api

# 2. Configure
cp config.example.json config.json
# Edit config.json with test accounts

# 3. Run backend
go run ./cmd/ds2api
# Default: http://localhost:5001
```

### Frontend Development (WebUI)

```bash
# 1. Navigate to WebUI directory
cd webui

# 2. Install dependencies
npm install

# 3. Start dev server (hot reload)
npm run dev
# Default: http://localhost:5173, auto-proxies API to backend
```

WebUI tech stack:
- React + Vite
- Tailwind CSS
- Bilingual language packs: `webui/src/locales/zh.json` / `en.json`

### Docker Development

```bash
docker-compose -f docker-compose.dev.yml up
```

## Code Standards

| Language | Standards |
| --- | --- |
| **Go** | Run `gofmt` and ensure `go test ./...` passes before committing |
| **JavaScript/React** | Follow existing project style (functional components) |
| **Commit messages** | Use semantic prefixes: `feat:`, `fix:`, `docs:`, `refactor:`, `style:`, `perf:`, `chore:` |

## Submitting a PR

1. Fork the repo
2. Create a branch (e.g. `feature/xxx` or `fix/xxx`)
3. Commit changes
4. Push your branch
5. Open a Pull Request

> 💡 If you modify files under `webui/`, no manual build is needed — CI handles it automatically.

## Build WebUI

Manually build WebUI to `static/admin/`:

```bash
./scripts/build-webui.sh
```

## Running Tests

```bash
# Go unit tests
go test ./...

# End-to-end live tests (real accounts)
./scripts/testsuite/run-live.sh
```

## Project Structure

```text
ds2api/
├── cmd/
│   ├── ds2api/              # Local/container entrypoint
│   └── ds2api-tests/        # End-to-end testsuite entrypoint
├── api/
│   ├── index.go             # Vercel Serverless Go entry
│   ├── chat-stream.js       # Vercel Node.js stream relay
│   └── helpers/             # Node.js helper modules
├── internal/
│   ├── account/             # Account pool and concurrency queue
│   ├── adapter/
│   │   ├── openai/          # OpenAI adapter
│   │   └── claude/          # Claude adapter
│   ├── admin/               # Admin API handlers
│   ├── auth/                # Auth and JWT
│   ├── config/              # Config loading and hot-reload
│   ├── deepseek/            # DeepSeek client, PoW WASM
│   ├── server/              # HTTP routing (chi router)
│   ├── sse/                 # SSE parsing utilities
│   ├── testsuite/           # Testsuite core logic
│   ├── util/                # Common utilities
│   └── webui/               # WebUI static hosting
├── webui/                   # React WebUI source
│   └── src/
│       ├── components/      # Components
│       └── locales/         # Language packs
├── scripts/                 # Build and test scripts
├── static/admin/            # WebUI build output (not committed)
├── Dockerfile               # Multi-stage build
├── docker-compose.yml       # Production
├── docker-compose.dev.yml   # Development
└── vercel.json              # Vercel config
```

## Reporting Issues

Please use [GitHub Issues](https://github.com/CJackHwang/ds2api/issues) and include:

- Steps to reproduce
- Relevant log output
- Environment info (OS, Go version, deployment method)

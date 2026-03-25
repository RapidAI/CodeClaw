# 贡献指南

语言 / Language: [中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

感谢你对 DS2API 的关注与贡献！

## 开发环境设置

### 前置要求

- Go 1.24+
- Node.js 20+（WebUI 开发时）
- npm（随 Node.js 提供）

### 后端开发

```bash
# 1. 克隆仓库
git clone https://github.com/CJackHwang/ds2api.git
cd ds2api

# 2. 配置
cp config.example.json config.json
# 编辑 config.json，填入测试账号

# 3. 启动后端
go run ./cmd/ds2api
# 默认监听 http://localhost:5001
```

### 前端开发（WebUI）

```bash
# 1. 进入 WebUI 目录
cd webui

# 2. 安装依赖
npm install

# 3. 启动开发服务器（热更新）
npm run dev
# 默认监听 http://localhost:5173，自动代理 API 到后端
```

WebUI 技术栈：
- React + Vite
- Tailwind CSS
- 中英文语言包：`webui/src/locales/zh.json` / `en.json`

### Docker 开发环境

```bash
docker-compose -f docker-compose.dev.yml up
```

## 代码规范

| 语言 | 规范 |
| --- | --- |
| **Go** | 提交前运行 `gofmt`，确保 `go test ./...` 通过 |
| **JavaScript/React** | 保持现有代码风格（函数组件） |
| **提交信息** | 使用语义化前缀：`feat:`、`fix:`、`docs:`、`refactor:`、`style:`、`perf:`、`chore:` |

## 提交 PR

1. Fork 仓库
2. 创建分支（如 `feature/xxx` 或 `fix/xxx`）
3. 提交更改
4. 推送分支
5. 发起 Pull Request

> 💡 如果修改了 `webui/` 目录下的文件，无需手动构建——CI 会自动处理。

## WebUI 构建

手动构建 WebUI 到 `static/admin/`：

```bash
./scripts/build-webui.sh
```

## 运行测试

```bash
# Go 单元测试
go test ./...

# 端到端全链路测试（真实账号）
./scripts/testsuite/run-live.sh
```

## 项目结构

```text
ds2api/
├── cmd/
│   ├── ds2api/              # 本地/容器启动入口
│   └── ds2api-tests/        # 端到端测试集入口
├── api/
│   ├── index.go             # Vercel Serverless Go 入口
│   ├── chat-stream.js       # Vercel Node.js 流式转发
│   └── helpers/             # Node.js 辅助模块
├── internal/
│   ├── account/             # 账号池与并发队列
│   ├── adapter/
│   │   ├── openai/          # OpenAI 兼容适配器
│   │   └── claude/          # Claude 兼容适配器
│   ├── admin/               # Admin API handlers
│   ├── auth/                # 鉴权与 JWT
│   ├── config/              # 配置加载与热更新
│   ├── deepseek/            # DeepSeek 客户端、PoW WASM
│   ├── server/              # HTTP 路由（chi router）
│   ├── sse/                 # SSE 解析工具
│   ├── testsuite/           # 测试集核心逻辑
│   ├── util/                # 通用工具
│   └── webui/               # WebUI 静态托管
├── webui/                   # React WebUI 源码
│   └── src/
│       ├── components/      # 组件
│       └── locales/         # 语言包
├── scripts/                 # 构建与测试脚本
├── static/admin/            # WebUI 构建产物（不提交）
├── Dockerfile               # 多阶段构建
├── docker-compose.yml       # 生产环境
├── docker-compose.dev.yml   # 开发环境
└── vercel.json              # Vercel 配置
```

## 问题反馈

请使用 [GitHub Issues](https://github.com/CJackHwang/ds2api/issues) 并附上：

- 复现步骤
- 相关日志输出
- 运行环境信息（OS、Go 版本、部署方式）

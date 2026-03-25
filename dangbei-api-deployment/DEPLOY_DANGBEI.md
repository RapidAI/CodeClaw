# Dangbei-API Docker 部署指南

本指南适用于定制版 `dangbei-api`（基于 DS2API 魔改，支持 OpenClaw 动态工具调用）。

## 1. 准备配置文件

在当前目录下确保存在 `config.json` 文件（如果没有可以参考原项目配置或复制示例文件）：

```json
[
  {
    "username": "你的手机号",
    "token": "你的抓包token"
  }
]
```

## 2. 启动服务

使用 Docker Compose 启动容器（会自动编译最新的定制版 Go 代码）：

```bash
docker-compose up -d --build
```

## 3. 查看日志

如果需要查看当贝 API 的请求和系统提示注入日志等，可以运行：

```bash
docker-compose logs -f
```

## 4. 停止与更新

停止服务：
```bash
docker-compose down
```

更新代码后热重载：
```bash
docker-compose up -d --build
```

## 注意事项

- 容器监听主机 `8080` 端口。请确保不要被其他系统服务（例如你之前安装的 `dangbei-api.service`）占用。
- 启动前，请通过 `sudo systemctl stop dangbei-api.service` 和 `sudo systemctl disable dangbei-api.service` 停用以前的本地进程版系统服务。
- 建议定期查看日志确认 Token 是否过期或遭官方风控拦截。

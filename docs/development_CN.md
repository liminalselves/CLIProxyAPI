# Windows 本地开发

## 环境

项目使用 Go 1.26+。本机已检查 Go 1.26.4、Git 2.54.0 和 Windows PowerShell 5.1；PowerShell 7 也可用。

控制台沿用原管理前端，构建后嵌入 Go 二进制。运行不需要前端服务、Docker、Redis 或 Postgres；修改前端后需先使用 Bun 构建，命令见 [前端构建说明](multi-user.md#frontend-source-and-build)。Go 依赖由 `go.mod` 和 `go.sum` 管理。

## 本机配置

项目根目录的 `config.yaml` 是本机运行配置：

- 服务地址为 `http://127.0.0.1:8317`，控制台入口为 `/management.html`，模型广场为 `/management.html#/marketplace`。
- 已启用 `multi-user`。账户和调用记录在 `local-data/hub/`，上游 OAuth 凭证在 `auths/`；两者分开存放。
- `.env` 中的 `MANAGEMENT_PASSWORD` 是本机管理 API 密码，不是账户登录密码，也不是模型 API Key。
- 多用户模式使用内嵌的原管理前端，不下载替换面板；旧 `/hub.html` 跳转到统一入口。
- `config.yaml`、`.env`、`auths/`、`local-data/` 和 `bin/` 内生成的内容均被 Git 忽略。

完整字段说明在 `config.example.yaml`。本地文件不存在时可复制模板，但需按上述方式设置地址、账户控制台和数据目录，移除 `access.api-keys` 中的示例密钥。全新账户数据不会自带管理员账号或上游凭证。

## 启动与停止

在项目目录打开 PowerShell，运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\dev.ps1
```

也可以双击项目根目录的 `start-dev.cmd`。启动入口不依赖当前终端目录，支持含中文或空格的路径。每次启动都会构建当前源码到 `bin/cli-proxy-api-dev.exe`，不会覆盖根目录已有的 `cli-proxy-api.exe`。保持终端打开，按 `Ctrl+C` 停止。

后台运行、查看进程和停止：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\dev.ps1 -Background
powershell -NoProfile -ExecutionPolicy Bypass -File .\dev.ps1 -Action status
powershell -NoProfile -ExecutionPolicy Bypass -File .\dev.ps1 -Action stop
```

后台运行输出保存在 `bin/dev-server.stdout.log` 和 `bin/dev-server.stderr.log`，下次后台启动会覆盖这两个日志。`status` 检查脚本记录的后台进程，不代表 HTTP 已就绪，也不跟踪前台启动。停止操作核对进程 ID、程序路径和启动时间，仅停止该脚本启动的后台实例。

源码没变时可加 `-NoBuild` 复用开发二进制。源码修改后必须去掉此参数重新构建。修改 `multi-user` 配置后需停止并重新启动；不要用两个实例同时打开同一个账户数据目录。

启动脚本与生产行为一致，远程模型目录正常刷新。需要离线开发、改用内嵌目录时可不经脚本直接运行：

```powershell
go run ./cmd/server --config ./config.yaml --local-model
```

## 访问与验证

```powershell
Invoke-RestMethod http://127.0.0.1:8317/healthz
Invoke-RestMethod http://127.0.0.1:8317/v8/hub/status
```

打开 [本地账户控制台](http://127.0.0.1:8317/management.html)。第一次使用时注册自己的账号，首个账号自动成为管理员。账号登录、创建个人 API Key、创建渠道和上传凭证的操作见 [多用户共享站指南](multi-user.md)。

客户端使用：

```text
Base URL: http://127.0.0.1:8317/v1
API Key: 在账户控制台创建的个人密钥
Model: 从模型市场复制的完整渠道模型 ID
```

没有上传真实上游凭证时，可以使用账户和管理页面，但模型市场没有可调用模型。仅凭服务启动或健康检查通过不能判定真实模型调用已成功。

依赖和源码构建检查：

```powershell
go mod download
go mod verify
go build -o ./bin/cli-proxy-api-dev.exe ./cmd/server
```

构建前先停止后台开发实例，避免 Windows 锁定正在运行的二进制。8317 端口占用时先确认占用进程，或修改 `config.yaml` 的 `server.port`，不要直接结束不明进程。

# Go 后端

启动、构建和配置见[根目录 README](../README.md)，旧数据导入与备份同样见根目录 README。

- `cmd/myanimego`：服务与导入命令。
- `internal/app`：组装服务、路由、后台任务与退出流程。
- `internal/server`：认证、订阅与 Transmission 管理 API。
- `internal/discovery`：Mikan、Anibt、Bangumi、TMDB 与 RSS 解析。
- `internal/pipeline`、`internal/scheduler`、`internal/jobs`：下载、文件命名和持久任务。
- `internal/storage`：SQLite 与事务。
- `internal/peerfilter`：公共规则转换、客户端识别与名单下发。
- `internal/notifications`：Webhook 与 Emby 刷新。
- `internal/filesapi`、`internal/migration`：附件、备份恢复与数据导入。
- `internal/apidocs`、`internal/mcpapi`：可选 OpenAPI 与 MCP。
- `internal/desktop`：可选原生托盘。
- `internal/webui`：发布时内嵌的 Vue 静态资源与开发目录覆盖。

业务状态保存在 SQLite，图片、种子与节点过滤快照存为文件；订阅队列与节点过滤各自管理生命周期。生产服务按各项任务设置运行；`-dev` 开发模式保留自动任务总暂停与审核入口。

## 启动参数

| 项目 | 参数 / 入口 |
| --- | --- |
| 数据目录 | `-data-dir` 或 `ANI_RSS_DATA_DIR` |
| HTTP 监听 | `-listen`，默认 `0.0.0.0:7789` |
| 外部前端目录 | `-web-dir`，覆盖发布程序内嵌的前端 |
| 开发模式 | `-dev`，保留自动任务暂停与审核入口 |
| OpenAPI | `SWAGGER_ENABLED=true`，访问 `/swagger-ui/index.html` |
| MCP | `MCP_ENABLED=true`，访问 `/api/mcp`，使用设置中的 API Key |
| 系统托盘 | `--gui`，macOS 托盘构建需要 cgo |

发布脚本先构建 Vue，再使用 `-tags webui` 将静态资源内嵌到 Go 程序。普通 `go run` / `go build` 不依赖前端产物，开发使用 Vite，或通过 `-web-dir ../frontend/dist` 提供已构建的页面。

`scripts/build.sh` 依次执行 `prepare-web.sh` 和 `build-backend.sh`。前者生成内嵌页面与前端许可，后者按 `GOOS`、`GOARCH`、`GOARM` 编译并收集目标平台的 Go 依赖许可；CI 将前端产物共享给各架构，避免重复构建。`package-release.py` 为 Windows 生成 ZIP，为 Linux/macOS 生成 tar.gz，每个包包含程序、文档和许可。

节点过滤中的 MyAnimeGo 地址必须能被 Transmission 访问，跨主机填写局域网或 Tailscale 地址。Bangumi 用于读取公开资料，Token 可留空。

## systemd 与媒体路径

使用 systemd 管理服务时，`ExecStart` 指定程序和数据目录的绝对路径，服务单元设置 `WantedBy=multi-user.target`，然后运行 `systemctl enable --now myanimego` 开启自启。

从容器迁移时，Go 与 Transmission 必须看到相同的下载路径。例如下载路径是 `/tv`、宿主机目录是 `/mnt/h1/app/emby/tv`，可在服务的 `[Service]` 中设置 `BindPaths=/mnt/h1/app/emby/tv:/tv`，在 `[Unit]` 中使用 `RequiresMountsFor=` 声明挂载盘依赖。

## 旧数据导入

先预览，再导入独立目录：

```bash
cd backend
go run ./cmd/myanimego preview -source /path/to/old-data-or-backup.zip
go run ./cmd/myanimego import -source /path/to/old-data-or-backup.zip -data-dir ./data/imported
```

导入过程保持任务暂停；核对订阅和下载路径后，启动生产服务即按各项设置运行任务。备份恢复入口位于设置页，手动复制数据目录前先停止服务。

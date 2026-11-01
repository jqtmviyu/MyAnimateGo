# API 文档

`openapi.json` 维护当前 Go 服务的接口、模型、认证与媒体类型。服务启动时按实际挂载路由筛选定义，缺少路径或 HTTP 方法时直接报告错误。新增、删除或调整接口时同步更新此文件，不依赖 Java 工程或 SpringDoc 导出。

设置 `SWAGGER_ENABLED=true` 后，访问 `/swagger-ui/index.html`；定义位于 `/api/v3/api-docs`。页面资源在 `assets/` 中随二进制内置，不访问在线 CDN，不保存授权信息。

Swagger UI 5.32.14 保留原 Apache-2.0 许可及第三方许可文件。静态定义来源于本项目现有接口契约，整理于 2026-11-01；旧版未挂载的路径与未引用模型已移除。

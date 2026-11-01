# 来源与开源许可

## 项目上游

本项目基于 [wushuo894/ani-rss](https://github.com/wushuo894/ani-rss) 改造，原作者 wushuo894 及其他贡献者的版权与署名保留。上游许可证为 [GPL v2](https://github.com/wushuo894/ani-rss/blob/main/LICENSE)，本项目沿用该协议，见根目录 [LICENSE](../LICENSE) 和未修改的[上游协议副本](ANI-RSS-GPL-2.0.txt)。

MyAnimeGo 由 jqtmviyu 维护。2026-11-01 整理为 Go 后端和 Vue 3 / shadcn-vue 前端，删除 Java/Maven 工程、旧发布脚本和原型，新增 Transmission 管理、节点过滤及明暗主题。删除旧工程不改变保留下来的代码与资源的许可归属；当前仓库以 MyAnimeGo 重新初始化，保留上述原作者署名和协议副本。

## 运行时规则数据

| 来源 | 作者 / 项目 | 许可 | 处理方式 |
| --- | --- | --- | --- |
| [BTN-Collected-Rules](https://github.com/PBH-BTN/BTN-Collected-Rules) | PBH-BTN | [CC BY 4.0](BTN-Collected-Rules-CC-BY-4.0.txt) | 读取 combine/all.txt，解析 IP/CIDR/范围，合并重叠并扣除允许地址，转换成 Transmission 名单 |
| [autorosvpn](https://github.com/zealic/autorosvpn) | Copyright (c) 2014-2024 Zealic | [MIT](autorosvpn-MIT.txt) | 使用电信、移动、联通/铁通地址集，转换并合并范围 |

规则文件在运行时下载，来源链接显示在节点过滤设置中；生成的名单附带来源、许可和转换说明。CC BY 4.0 数据与 MIT 数据的许可分别适用，不被项目 GPL 说明替换。

## 设计与功能参考

- [TrguiNG](https://github.com/openscopeproject/TrguiNG)：Transmission 功能和 RPC 交互参考，许可证为 [AGPL v3](https://github.com/openscopeproject/TrguiNG/blob/master/LICENSE.txt)。本版本提供自己的 Vue 界面与 Go RPC 接口，没有嵌入或分发 TrguiNG 的前端包、桌面包。
- [PeerBanHelper](https://github.com/PBH-BTN/PeerBanHelper)：节点识别与过滤机制参考，实际公共数据许可按上表处理。
- [qBittorrent-ClientBlocker](https://github.com/Simple-Tracker/qBittorrent-ClientBlocker)：参考其 Transmission 节点采集、临时封禁与名单下发机制。采用 MIT License，Copyright (c) 2023 Simple-Tracker；保留[许可原文副本](qBittorrent-ClientBlocker-MIT.txt)，来源为[上游 LICENSE](https://github.com/Simple-Tracker/qBittorrent-ClientBlocker/blob/dev/LICENSE)。
- Emby：界面布局与明暗配色参考，项目不分发 Emby 的代码或界面资产。

## 依赖与内置资源

Vue、shadcn-vue、Reka UI、Tailwind CSS、Lucide、SQLite 驱动及其他依赖仍按各自许可证使用，确切版本保存在 `frontend/pnpm-lock.yaml` 与 `backend/go.sum`。其中 `@vue/devtools-api@6.6.4` 的发布包未附许可文件，保留[对应上游标签的 MIT 许可副本](vue-devtools-MIT.txt)，来源为 [vuejs/devtools-v6 v6.6.4](https://github.com/vuejs/devtools-v6/blob/v6.6.4/LICENSE)。内置 Swagger UI 保留其 [Apache 2.0 许可证](../backend/internal/apidocs/assets/LICENSE) 与 [第三方许可清单](../backend/internal/apidocs/assets/swagger-ui-bundle.js.LICENSE.txt)。

二进制打包入口会携带项目许可证、本目录和对应依赖许可清单。分发修改版本时，应同时提供该版本对应源代码及构建说明，保留各来源的许可和署名。

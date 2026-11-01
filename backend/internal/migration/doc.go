// Package migration 识别 1.x 与 3.x 配置目录或 ZIP，提供 Preview(source string)
// 和 Import(ctx, store, source)。目录导入包含 files、torrents、cache；ZIP 只包含 files、torrents。
// 预览报告只含字段名与转换说明，导入后自动任务暂停。附件先暂存于数据库目录，跨文件系统安装会失败；
// SQLite 与文件系统无法组成共同事务，进程或系统崩溃可能留下未关联文件。
package migration

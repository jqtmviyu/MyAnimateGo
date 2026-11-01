//go:build webui

package webui

import (
	"embed"
	"io/fs"
)

// 发布脚本先生成此目录；内嵌资源直接读取，不向数据目录解压。
//
//go:embed all:dist
var assets embed.FS

func bundled() (fs.FS, error) {
	return fs.Sub(assets, "dist")
}

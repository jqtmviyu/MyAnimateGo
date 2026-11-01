//go:build !webui

package webui

import "io/fs"

// 后端开发无需预先构建前端，页面由 Vite 或 -web-dir 提供。
func bundled() (fs.FS, error) {
	return nil, nil
}

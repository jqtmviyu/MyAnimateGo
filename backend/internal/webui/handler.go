// Package webui 提供发布包内嵌前端，以及开发时的外部静态目录。
package webui

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
)

// Handler 优先使用显式指定的目录，否则使用构建时嵌入的资源。
func Handler(directory string) (http.Handler, error) {
	var files fs.FS
	if directory != "" {
		files = os.DirFS(directory)
	} else {
		var err error
		files, err = bundled()
		if err != nil {
			return nil, fmt.Errorf("读取内嵌前端失败：%w", err)
		}
		if files == nil {
			return http.NotFoundHandler(), nil
		}
	}
	entry, err := fs.Stat(files, "index.html")
	if err != nil {
		return nil, fmt.Errorf("前端目录缺少可读的 index.html：%w", err)
	}
	if !entry.Mode().IsRegular() {
		return nil, fmt.Errorf("前端 index.html 必须是普通文件")
	}
	static := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 入口每次重新验证，升级后浏览器会读取当前二进制里的资源地址。
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		static.ServeHTTP(w, r)
	}), nil
}

//go:build (!darwin && !windows && !linux) || (darwin && !cgo) || ios || android

package desktop

import (
	"context"
	"fmt"
	"io"
)

// Run 在未包含原生托盘的平台继续启动命令行服务。
func Run(ctx context.Context, service Service, _ string, stderr io.Writer) error {
	fmt.Fprintln(stderr, "此构建不包含系统托盘，继续以命令行方式运行")
	return service(ctx, func(string) {})
}

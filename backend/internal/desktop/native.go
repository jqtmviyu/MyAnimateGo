//go:build (darwin && cgo && !ios) || windows || (linux && !android)

package desktop

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"runtime"

	"fyne.io/systray"
)

//go:embed icon.png
var iconPNG []byte

//go:embed icon.ico
var iconICO []byte

// Run 在主线程运行托盘；菜单退出和系统信号共用服务的停止流程。
func Run(ctx context.Context, service Service, dataDir string, stderr io.Writer) error {
	if !available() {
		fmt.Fprintln(stderr, "系统托盘不可用，继续以命令行方式运行")
		return service(ctx, func(string) {})
	}
	return runWithTray(ctx, service, dataDir, stderr, nativeTray{}, openTarget)
}

type nativeTray struct{}

func (nativeTray) Run(ready func(controls), exit func()) {
	systray.Run(func() {
		icon := iconPNG
		if runtime.GOOS == "windows" {
			icon = iconICO
		}
		systray.SetIcon(icon)
		systray.SetTooltip("MyAnimeGo")
		web := systray.AddMenuItem("WebUI", "打开管理页面")
		web.Disable()
		config := systray.AddMenuItem("Config", "打开 Go 数据目录")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Quit", "停止服务并退出")
		ready(controls{web: web.ClickedCh, config: config.ClickedCh, quit: quit.ClickedCh, listening: web.Enable})
	}, exit)
}

func (nativeTray) Quit() { systray.Quit() }

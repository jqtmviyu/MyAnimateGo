// Package desktop 将原生托盘菜单连接到服务的启动与优雅停止流程。
package desktop

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Service 在每次监听成功后报告实际地址，直到服务停止。
type Service func(context.Context, func(string)) error

type controls struct {
	web, config, quit <-chan struct{}
	listening         func()
}

type tray interface {
	Run(func(controls), func())
	Quit()
}

func runWithTray(ctx context.Context, service Service, dataDir string, stderr io.Writer, ui tray, open func(context.Context, string) error) error {
	directory, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("定位数据目录失败：%w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	ui.Run(func(menu controls) {
		go func() {
			result <- operate(ctx, cancel, service, directory, stderr, menu, open)
			ui.Quit()
		}()
	}, cancel)
	cancel()
	return <-result
}

func operate(ctx context.Context, cancel context.CancelFunc, service Service, directory string, stderr io.Writer, menu controls, open func(context.Context, string) error) error {
	addresses := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- service(ctx, func(address string) {
			select {
			case addresses <- address:
			case <-ctx.Done():
			}
		})
	}()
	var webURL string
	for {
		var target string
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return <-done
		case <-menu.quit:
			// 先等待工作者、HTTP 与 SQLite 退出，再移除托盘。
			cancel()
			return <-done
		case address := <-addresses:
			webURL = localURL(address)
			menu.listening()
			continue
		case <-menu.web:
			if webURL == "" {
				continue
			}
			target = webURL
		case <-menu.config:
			target = directory
		}
		if err := open(ctx, target); err != nil && ctx.Err() == nil {
			fmt.Fprintf(stderr, "打开托盘入口失败：%v\n", err)
		}
	}
}

func localURL(address string) string {
	host, port, _ := net.SplitHostPort(address)
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		if ip.To4() != nil {
			host = "127.0.0.1"
		} else {
			host = "::1"
		}
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}

func openTarget(ctx context.Context, target string) error {
	command, args := openCommand(runtime.GOOS, target)
	return exec.CommandContext(ctx, command, args...).Run()
}

func openCommand(goos, target string) (string, []string) {
	switch goos {
	case "darwin":
		return "/usr/bin/open", []string{"--", target}
	case "windows":
		return "rundll32.exe", []string{"url.dll,FileProtocolHandler", target}
	default:
		return "xdg-open", []string{target}
	}
}

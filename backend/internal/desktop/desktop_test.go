package desktop

import (
	"context"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type testTray struct {
	web, config, quit chan struct{}
	listening         chan struct{}
	closed            chan struct{}
	stopped           *atomic.Bool
	cleanOnClose      atomic.Bool
}

func newTestTray(stopped *atomic.Bool) *testTray {
	return &testTray{web: make(chan struct{}), config: make(chan struct{}), quit: make(chan struct{}), listening: make(chan struct{}, 2), closed: make(chan struct{}), stopped: stopped}
}

func (ui *testTray) Run(ready func(controls), exit func()) {
	ready(controls{web: ui.web, config: ui.config, quit: ui.quit, listening: func() { ui.listening <- struct{}{} }})
	<-ui.closed
	exit()
}

func (ui *testTray) Quit() {
	ui.cleanOnClose.Store(ui.stopped.Load())
	close(ui.closed)
}

func TestTrayMenusFollowListenerAndWaitForServiceCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stopped atomic.Bool
	ui := newTestTray(&stopped)
	restart := make(chan struct{})
	service := func(ctx context.Context, ready func(string)) error {
		defer stopped.Store(true)
		ready("0.0.0.0:7789")
		select {
		case <-restart:
			ready("[::]:7790")
		case <-ctx.Done():
			return ctx.Err()
		}
		<-ctx.Done()
		return nil
	}
	opened := make(chan string, 3)
	directory := filepath.Join(t.TempDir(), "配置目录 with spaces")
	done := make(chan error, 1)
	go func() {
		done <- runWithTray(ctx, service, directory, io.Discard, ui, func(_ context.Context, target string) error {
			opened <- target
			return nil
		})
	}()
	checkOpen := func(action chan struct{}, expected string) {
		t.Helper()
		select {
		case action <- struct{}{}:
		case <-ctx.Done():
			t.Fatal("menu event timed out")
		}
		select {
		case actual := <-opened:
			if actual != expected {
				t.Fatalf("opened %q, want %q", actual, expected)
			}
		case <-ctx.Done():
			t.Fatal("open action timed out")
		}
	}
	waitReady := func() {
		t.Helper()
		select {
		case <-ui.listening:
		case <-ctx.Done():
			t.Fatal("listening action timed out")
		}
	}
	waitReady()
	checkOpen(ui.web, "http://127.0.0.1:7789/")
	checkOpen(ui.config, directory)
	close(restart)
	waitReady()
	checkOpen(ui.web, "http://[::1]:7790/")
	select {
	case ui.quit <- struct{}{}:
	case <-ctx.Done():
		t.Fatal("quit action timed out")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("service cleanup timed out")
	}
	if !ui.cleanOnClose.Load() {
		t.Fatal("tray closed before service cleanup")
	}
}

func TestSignalStopsServiceAndTray(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stopped atomic.Bool
	ui := newTestTray(&stopped)
	done := make(chan error, 1)
	go func() {
		done <- runWithTray(ctx, func(ctx context.Context, ready func(string)) error {
			defer stopped.Store(true)
			ready("127.0.0.1:7789")
			<-ctx.Done()
			return nil
		}, t.TempDir(), io.Discard, ui, openTarget)
	}()
	select {
	case <-ui.listening:
	case <-time.After(5 * time.Second):
		t.Fatal("listener not ready")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("signal did not stop tray")
	}
	if !ui.cleanOnClose.Load() {
		t.Fatal("signal closed tray before service cleanup")
	}
}

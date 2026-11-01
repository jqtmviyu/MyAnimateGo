package instance_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/instance"
)

func TestOnlyOneInstanceUsesDataDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.lock")
	first, err := instance.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := instance.Acquire(path)
	if second != nil {
		_ = second.Close()
		t.Fatal("第二个实例不应取得锁")
	}
	if !errors.Is(err, instance.ErrAlreadyRunning) {
		t.Fatalf("应明确报告已有实例：%v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := instance.Acquire(path)
	if err != nil {
		t.Fatalf("释放后应能重新启动：%v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
}

package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrAlreadyRunning = errors.New("该数据目录已有运行实例")

type Lock struct{ file *os.File }

// 使用系统文件锁互斥，进程退出后由系统释放，不依赖残留 PID 文件判断。
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("创建数据目录：%w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开实例锁：%w", err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("获取实例锁：%w", err)
	}
	return &Lock{file: file}, nil
}

func (lock *Lock) Close() error {
	err := unlockFile(lock.file)
	return errors.Join(err, lock.file.Close())
}

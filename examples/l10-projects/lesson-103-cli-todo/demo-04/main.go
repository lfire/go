package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

var ErrLockTimeout = errors.New("获取文件锁超时")

// FileLock 用 advisory lock 把读改写跨进程串行化。
type FileLock struct {
	path string
	f    *os.File
}

func (l *FileLock) Acquire(timeout time.Duration) error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("打开锁文件: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			l.f = f
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			f.Close()
			return fmt.Errorf("加锁失败: %w", err)
		}
		if time.Now().After(deadline) {
			f.Close()
			return ErrLockTimeout
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (l *FileLock) Release() {
	if l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}

// WithLock 把临界区包在获取与释放之间，保证任何路径都解锁。
func WithLock(path string, timeout time.Duration, fn func() error) error {
	l := &FileLock{path: path}
	if err := l.Acquire(timeout); err != nil {
		return err
	}
	defer l.Release()
	return fn()
}

func main() {
	dir, _ := os.MkdirTemp("", "todo-")
	defer os.RemoveAll(dir)
	lock := dir + "/todo.lock"
	err := WithLock(lock, 500*time.Millisecond, func() error {
		fmt.Println("临界区: 独占读改写 todo.json")
		return nil
	})
	fmt.Println("结果:", err)
}

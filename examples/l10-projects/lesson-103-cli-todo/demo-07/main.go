package main

import (
	"errors"
	"fmt"
	"os"
)

var ErrUsage = errors.New("参数错误")
var ErrNotFound = errors.New("任务不存在")
var ErrCorrupted = errors.New("数据文件损坏")
var ErrBusy = errors.New("文件锁超时")

const (
	ExitOK       = 0
	ExitInternal = 1
	ExitUsage    = 2
	ExitNotFound = 3
	ExitBusy     = 4
)

// exitCode 把错误类别映射到稳定的退出码，脚本据此决定重试还是报错。
func exitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, ErrUsage):
		return ExitUsage
	case errors.Is(err, ErrNotFound):
		return ExitNotFound
	case errors.Is(err, ErrBusy):
		return ExitBusy
	default:
		return ExitInternal
	}
}

// fail 统一把人类可读的错误写到 stderr，绝不污染 stdout 的数据流。
func fail(err error) {
	fmt.Fprintf(os.Stderr, "todo: %v\n", err)
}

func complete(id int) error {
	if id <= 0 {
		return fmt.Errorf("%w: id 必须为正整数", ErrUsage)
	}
	if id > 100000 {
		return ErrNotFound
	}
	return nil
}

func main() {
	for _, id := range []int{0, 7, 999999} {
		err := complete(id)
		code := exitCode(err)
		if err != nil {
			fail(err)
		}
		fmt.Printf("id=%d -> exit=%d\n", id, code)
	}
}

package main

import (
	"fmt"
	"os"
)

// logf 是自定义的 printf-like 包装，go vet 默认不认识它
func logf(format string, args ...any) {
	fmt.Printf("[app] "+format+"\n", args...)
}

func readConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close() // errcheck 会关注 Close 的错误是否被忽略
	logf("opened %s", path)
	return nil
}

func main() {
	if err := readConfig("config.yaml"); err != nil {
		logf("config error: %v", err)
	}
}

// 发布制品校验：为二进制生成 SHA-256 摘要
package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

func checksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func main() {
	// 演示：对当前可执行文件自身求摘要
	self, err := os.Executable()
	if err != nil {
		fmt.Println("定位可执行文件失败:", err)
		return
	}
	sum, err := checksum(self)
	if err != nil {
		fmt.Println("计算摘要失败:", err)
		return
	}
	fmt.Println("摘要长度:", len(sum))
}

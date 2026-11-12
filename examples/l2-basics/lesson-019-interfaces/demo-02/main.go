package main

import (
	"fmt"
	"strings"
)

type Reader interface {
	Read() string
}

type Closer interface {
	Close()
}

// 接口组合：把小接口拼起来
type ReadCloser interface {
	Reader
	Closer
}

type File struct{ name string }

func (f File) Read() string { return "data from " + f.name }
func (f File) Close()       { fmt.Println("closed", f.name) }

func use(rc ReadCloser) {
	fmt.Println(rc.Read())
	rc.Close()
}

func main() {
	use(File{name: "a.txt"})

	// strings.Reader 满足 io.Reader
	r := strings.NewReader("abc")
	buf := make([]byte, 3)
	n, _ := r.Read(buf)
	fmt.Println("读到", n, "字节:", string(buf[:n]))
}

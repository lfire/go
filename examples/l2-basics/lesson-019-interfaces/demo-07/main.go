package main

import (
	"errors"
	"fmt"
)

type Reader interface{ Read() string }

type fileReader struct{ data string }

func (f *fileReader) Read() string { return f.data }

func open(ok bool) (Reader, error) {
	if !ok {
		return nil, errors.New("打开失败")
	}
	return &fileReader{data: "hello"}, nil
}

type Code int

func (c Code) String() string { return fmt.Sprintf("CODE-%d", int(c)) }

func stringify(v any) string {
	switch x := v.(type) {
	case fmt.Stringer:
		return x.String()
	case nil:
		return "空值"
	default:
		return fmt.Sprint(x)
	}
}

func main() {
	var p *fileReader
	var i Reader = p
	fmt.Println("装入 nil 指针后 i == nil =", i == nil, "动态类型 =", fmt.Sprintf("%T", i))

	bad, err := open(false)
	fmt.Println("显式 nil =", bad == nil, "出错 =", err != nil)

	fmt.Println("能力分支 =", stringify(Code(7)))
	fmt.Println("空值分支 =", stringify(nil))
}

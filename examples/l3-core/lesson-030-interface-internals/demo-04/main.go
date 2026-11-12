package main

import (
	"fmt"
	"io"
	"os"
)

//go:noinline
func box(x int) any { return x }

//go:noinline
func viaIface(w io.Writer, b []byte) (int, error) { return w.Write(b) }

//go:noinline
func assertWriter(v any) (io.Writer, bool) {
	w, ok := v.(io.Writer)
	return w, ok
}

func main() {
	v := box(42)
	_, _ = viaIface(os.Stdout, []byte("x"))
	_, _ = assertWriter(v)
	fmt.Println(v)
}

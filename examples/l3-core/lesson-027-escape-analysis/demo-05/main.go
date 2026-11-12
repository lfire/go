package main

var global *int

//go:noinline
func leakContent(p **int) { global = *p } // 内容外泄，derefs=1

//go:noinline
func leakSelf(p *int) { global = p } // 指针本身外泄，derefs=0

func main() {
	x := 1
	p := &x
	leakContent(&p)
	leakSelf(p)
	_ = global
}

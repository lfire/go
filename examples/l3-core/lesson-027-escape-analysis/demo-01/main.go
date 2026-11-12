package main

type User struct {
	ID   int
	Name string
}

//go:noinline
func stackUser() User { // 返回的是值拷贝，不因「返回」而逃逸
	u := User{ID: 1, Name: "a"}
	return u
}

//go:noinline
func heapUser() *User { // 返回局部变量地址，u 必须逃逸
	u := User{ID: 2, Name: "b"}
	return &u
}

func main() {
	a := stackUser()
	b := heapUser()
	// 刻意不把结果传给 fmt.Println：...any 会让参数逃逸，干扰观察
	_ = a
	_ = b
}

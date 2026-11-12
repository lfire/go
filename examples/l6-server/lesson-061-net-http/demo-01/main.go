package main

import "net/http"

// Handler 是 net/http 的核心抽象：只约定一个方法。
type Handler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func main() {
	// HandlerFunc 把普通函数适配成 Handler，编译期即可验证接口满足。
	var _ http.Handler = http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {},
	)
}

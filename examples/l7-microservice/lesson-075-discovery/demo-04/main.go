package main

import "fmt"

// resolve 在注册中心报错时回退到最后一份本地缓存。
func resolve(remote []string, err error, cache []string) []string {
	if err != nil {
		return cache
	}
	return remote
}

func main() {
	cache := []string{"10.0.0.1:9000", "10.0.0.2:9000"}
	fmt.Println("正常:", resolve([]string{"10.0.0.1:9000"}, nil, cache))
	fmt.Println("抖动:", resolve(nil, fmt.Errorf("registry timeout"), cache))
}

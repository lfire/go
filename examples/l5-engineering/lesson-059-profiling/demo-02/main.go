// 引入 net/http/pprof 后会注册诊断端点，务必只挂内网端口
package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof" // 副作用导入：把处理器注册到 DefaultServeMux
	"net/http/httptest"
	"strings"
)

func main() {
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(rec, req)
	fmt.Println("诊断端点状态码:", rec.Code)
	fmt.Println("是否列出 heap:", strings.Contains(rec.Body.String(), "heap"))
}

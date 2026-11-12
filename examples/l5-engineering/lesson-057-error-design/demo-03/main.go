// 统一响应：内部错误映射到对外状态，细节只进日志
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
)

type Code int

const (
	CodeNotFound Code = 1001
	CodeInternal Code = 1500
)

// Response 是前后端共用的响应契约
type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

// statusOf 把内部错误码映射成 HTTP 状态码
func statusOf(c Code) int {
	switch c {
	case CodeNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// writeErr 只把安全提示写给客户端，内部细节写日志
func writeErr(w http.ResponseWriter, c Code, logMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusOf(c))
	msg := "请求失败"
	if c == CodeNotFound {
		msg = "资源不存在"
	}
	_ = json.NewEncoder(w).Encode(Response{Code: int(c), Msg: msg})
	fmt.Println("internal log:", logMsg)
}

func main() {
	rec := httptest.NewRecorder()
	writeErr(rec, CodeNotFound, "sql: no rows in order table")
	fmt.Println("状态码:", rec.Code)
	fmt.Println("响应体:", rec.Body.String())
}

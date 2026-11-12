package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// RPCFunc 抽象一次内部调用：网关把 HTTP 请求翻译成方法与参数。
type RPCFunc func(method string, params map[string]any) (any, error)

func invoke(method string, params map[string]any) (any, error) {
	switch method {
	case "Order.Get":
		return map[string]any{"order_id": params["id"], "status": "paid"}, nil
	default:
		return nil, fmt.Errorf("unknown method")
	}
}

// ProtocolBridge 做协议转换：REST 路径转 RPC 方法，结果再包成 JSON 回写。
func ProtocolBridge(invoke RPCFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string]any{}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 2 {
			params["id"] = parts[len(parts)-1] // 取路径末段作为订单号
		}
		result, err := invoke("Order.Get", params)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}

func main() {
	ts := httptest.NewServer(ProtocolBridge(invoke))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/order/10086")
	if err != nil {
		fmt.Println("请求失败:", err)
		return
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	fmt.Printf("协议转换结果: 状态=%d 数据=%v\n", resp.StatusCode, out)
}

package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// field 描述一个请求字段；schema 与校验规则共用同一份元数据。
type field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

func schema(fields []field) map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, f := range fields {
		props[f.Name] = map[string]any{"type": f.Type}
		if f.Required {
			required = append(required, f.Name)
		}
	}
	return map[string]any{
		"type": "object", "properties": props, "required": required,
	}
}

// openapi 由元数据生成文档，避免「实现改了文档没改」。
func openapi() map[string]any {
	userSchema := schema([]field{
		{Name: "name", Type: "string", Required: true},
		{Name: "email", Type: "string", Required: true},
		{Name: "age", Type: "integer"},
	})
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "User API", "version": "1.0.0"},
		"paths": map[string]any{
			"/v1/users": map[string]any{
				"post": map[string]any{
					"summary": "创建用户",
					"requestBody": map[string]any{
						"content": map[string]any{
							"application/json": map[string]any{"schema": userSchema},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{"description": "已创建"},
					},
				},
			},
		},
	}
}

func main() {
	mux := http.NewServeMux()

	// 暴露机器可读契约，供代码生成、Mock 与契约测试消费。
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(openapi())
	})

	// 版本放在 URL 前缀，最易在网关做路由与灰度。
	mux.HandleFunc("GET /v1/users", v1Users)
	mux.HandleFunc("GET /v2/users", v2Users)

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func v1Users(w http.ResponseWriter, r *http.Request) {
	// 弃用声明：给客户端明确的迁移信号，而不是悄悄改语义。
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Sunset", "Wed, 31 Dec 2025 23:59:59 GMT")
	writeList(w, []string{"gopher"})
}

func v2Users(w http.ResponseWriter, r *http.Request) {
	writeList(w, []map[string]any{{"id": 1, "displayName": "gopher"}})
}

func writeList(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"data": v})
}

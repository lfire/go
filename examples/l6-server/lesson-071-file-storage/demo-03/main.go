package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var signKey = []byte("replace-with-object-storage-secret")

const endpoint = "oss.example.com" // 生产从配置读取

// sign 对「方法 + 对象键 + 过期时间」签名，客户端凭它直传对象存储。
func sign(method, object string, expire int64) string {
	raw := fmt.Sprintf("%s\n%s\n%d", method, object, expire)
	mac := hmac.New(sha256.New, signKey)
	_, _ = mac.Write([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verify(method, object string, expire int64, sig string) error {
	if time.Now().Unix() > expire {
		return fmt.Errorf("credential expired")
	}
	want := sign(method, object, expire)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return fmt.Errorf("bad signature")
	}
	return nil
}

func main() {
	mux := http.NewServeMux()
	// 应用服务器只签发凭证，字节流不经过它，带宽与内存压力都转移出去。
	mux.HandleFunc("POST /presign", func(w http.ResponseWriter, r *http.Request) {
		object := r.URL.Query().Get("object")
		// 对象键必须白名单校验：禁止 .. 路径穿越与任意前缀。
		if object == "" || strings.Contains(object, "..") {
			http.Error(w, "invalid object", http.StatusBadRequest)
			return
		}
		expire := time.Now().Add(15 * time.Minute).Unix()
		writeJSON(w, http.StatusOK, map[string]any{
			"url":       endpoint + "/" + object,
			"expire":    expire,
			"signature": sign("PUT", object, expire),
		})
	})
	// 模拟对象存储侧的校验入口，演示服务端如何验签。
	mux.HandleFunc("PUT /verify", func(w http.ResponseWriter, r *http.Request) {
		object := r.URL.Query().Get("object")
		exp, _ := strconv.ParseInt(
			r.URL.Query().Get("expire"), 10, 64)
		sig := r.URL.Query().Get("signature")
		if err := verify("PUT", object, exp, sig); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		fmt.Fprintln(w, "signature ok, upload allowed")
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

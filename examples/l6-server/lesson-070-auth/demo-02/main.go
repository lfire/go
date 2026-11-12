package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

var secret = []byte("replace-with-a-long-random-secret")

type Claims struct {
	Sub  string `json:"sub"`
	Role string `json:"role"`
	JTI  string `json:"jti"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

func sign(head, payload string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(head + "." + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func Issue(c Claims) (string, error) {
	headBytes, _ := json.Marshal(map[string]string{
		"alg": "HS256", "typ": "JWT",
	})
	bodyBytes, _ := json.Marshal(c)
	head := base64.RawURLEncoding.EncodeToString(headBytes)
	body := base64.RawURLEncoding.EncodeToString(bodyBytes)
	return head + "." + body + "." + sign(head, body), nil
}

func Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("malformed token")
	}
	// 必须固定算法：否则攻击者可把 alg 改成 none 绕过签名。
	headBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, err
	}
	var head struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headBytes, &head); err != nil {
		return Claims{}, err
	}
	if head.Alg != "HS256" {
		return Claims{}, fmt.Errorf("unexpected alg")
	}
	// hmac.Equal 是恒定时间比较，避免时序侧信道。
	want := sign(parts[0], parts[1])
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return Claims{}, fmt.Errorf("bad signature")
	}
	bodyBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, err
	}
	var c Claims
	if err := json.Unmarshal(bodyBytes, &c); err != nil {
		return Claims{}, err
	}
	if time.Now().Unix() > c.Exp {
		return Claims{}, fmt.Errorf("token expired")
	}
	return c, nil
}

type ctxKey string

const claimsKey ctxKey = "claims"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", login)
	mux.HandleFunc("GET /me", authMiddleware(me))
	mux.HandleFunc("GET /admin",
		authMiddleware(requireRole("admin", admin)))
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func login(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	jti := fmt.Sprint(now.UnixNano())
	access, _ := Issue(Claims{
		Sub: "u-1", Role: "admin", JTI: "a-" + jti,
		Iat: now.Unix(), Exp: now.Add(15 * time.Minute).Unix(),
	})
	refresh, _ := Issue(Claims{
		Sub: "u-1", JTI: "r-" + jti,
		Iat: now.Unix(), Exp: now.Add(7 * 24 * time.Hour).Unix(),
	})
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token": access, "refresh_token": refresh,
	})
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token := strings.TrimPrefix(h, "Bearer ")
		if token == h { // 没有 Bearer 前缀
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		c, err := Verify(token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, c)
		next(w, r.WithContext(ctx))
	}
}

func requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := r.Context().Value(claimsKey).(Claims)
		if !ok || c.Role != role {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func me(w http.ResponseWriter, r *http.Request) {
	c := r.Context().Value(claimsKey).(Claims)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"sub": c.Sub, "role": c.Role,
	})
}

func admin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("admin area"))
}

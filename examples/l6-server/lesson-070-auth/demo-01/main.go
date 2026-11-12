package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

type Session struct {
	UserID string
	Role   string
	Expiry time.Time
}

type Store struct {
	mu   sync.RWMutex
	data map[string]Session
}

func (s *Store) Create(userID, role string, ttl time.Duration) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf) // 用 CSPRNG，绝不用 math/rand 生成令牌
	sid := hex.EncodeToString(buf)
	s.mu.Lock()
	s.data[sid] = Session{
		UserID: userID, Role: role, Expiry: time.Now().Add(ttl),
	}
	s.mu.Unlock()
	return sid
}

func (s *Store) Get(sid string) (Session, bool) {
	s.mu.RLock()
	v, ok := s.data[sid]
	s.mu.RUnlock()
	if !ok || time.Now().After(v.Expiry) {
		return Session{}, false
	}
	return v, true
}

func (s *Store) Delete(sid string) {
	s.mu.Lock()
	delete(s.data, sid)
	s.mu.Unlock()
}

var store = &Store{data: make(map[string]Session)}

const cookieName = "sid"

type ctxKey string

const userKey ctxKey = "user"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", login)
	mux.HandleFunc("POST /logout", logout)
	mux.HandleFunc("GET /me", requireAuth(me))
	mux.HandleFunc("GET /admin",
		requireAuth(requireRole("admin", admin)))
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User string `json:"user"`
		Pass string `json:"pass"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	// 演示用；生产应查库并用 bcrypt 比对密码哈希。
	if req.User != "gopher" || req.Pass != "secret" {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	sid := store.Create("u-1", "admin", 30*time.Minute)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,                 // JS 读不到，缓解 XSS 窃取
		Secure:   r.TLS != nil,         // 生产 HTTPS 下必须为 true
		SameSite: http.SameSiteLaxMode, // 缓解 CSRF
		Expires:  time.Now().Add(30 * time.Minute),
	})
	w.WriteHeader(http.StatusNoContent)
}

func logout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(cookieName)
	if err == nil {
		store.Delete(c.Value) // 服务端删状态，令牌立即失效
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true,
	})
	w.WriteHeader(http.StatusNoContent)
}

// requireAuth 是认证：确认「你是谁」。
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sess, ok := store.Get(c.Value)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// 身份只放进本次请求作用域的 context，随请求结束而消失。
		ctx := context.WithValue(r.Context(), userKey, sess)
		next(w, r.WithContext(ctx))
	}
}

// requireRole 是授权：确认「你能做什么」。
func requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := r.Context().Value(userKey).(Session)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if sess.Role != role {
			http.Error(w, "forbidden", http.StatusForbidden) // 403 不是 401
			return
		}
		next(w, r)
	}
}

func me(w http.ResponseWriter, r *http.Request) {
	sess := r.Context().Value(userKey).(Session)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"user_id": sess.UserID, "role": sess.Role,
	})
}

func admin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("admin area"))
}

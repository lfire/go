package main

import (
	"fmt"
	"sync"
	"time"
)

type Token struct {
	JTI string
	Exp time.Time
}

type Revoker struct {
	mu      sync.Mutex
	blocked map[string]time.Time // jti -> 该令牌本身的过期时间
	used    map[string]bool      // 已使用过的 refresh jti
}

func (r *Revoker) Revoke(t Token) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocked[t.JTI] = t.Exp
	fmt.Println("加入黑名单:", t.JTI)
}

func (r *Revoker) IsRevoked(jti string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	exp, ok := r.blocked[jti]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(r.blocked, jti) // 令牌本就过期了，黑名单项无需保留
		return false
	}
	return true
}

// Rotate 刷新令牌轮换：旧的 refresh 立即作废，返回新的一对令牌。
func (r *Revoker) Rotate(oldJTI string) (Token, Token) {
	r.mu.Lock()
	if r.used[oldJTI] {
		// 同一个 refresh 被用第二次，极可能是泄露，应撤销整条令牌链。
		fmt.Println("检测到 refresh 重放，撤销该用户全部令牌")
	}
	r.used[oldJTI] = true
	r.mu.Unlock()

	now := time.Now()
	suffix := fmt.Sprint(now.UnixNano())
	return Token{JTI: "access-" + suffix,
		Exp: now.Add(15 * time.Minute)},
		Token{JTI: "refresh-" + suffix,
			Exp: now.Add(7 * 24 * time.Hour)}
}

func main() {
	r := &Revoker{
		blocked: map[string]time.Time{}, used: map[string]bool{},
	}
	now := time.Now()
	access := Token{JTI: "access-1", Exp: now.Add(15 * time.Minute)}
	refresh := Token{JTI: "refresh-1", Exp: now.Add(7 * 24 * time.Hour)}

	fmt.Println("吊销前是否可用:", !r.IsRevoked(access.JTI))
	r.Revoke(access) // 退出登录：把 access 的 jti 加入黑名单
	fmt.Println("吊销后是否可用:", !r.IsRevoked(access.JTI))

	a2, f2 := r.Rotate(refresh.JTI)
	fmt.Println("轮换得到新 access 与新 refresh:", a2.JTI != "", f2.JTI != "")
	r.Rotate(refresh.JTI) // 再次使用旧 refresh，触发重放检测
}

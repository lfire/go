package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type Signer struct {
	secret []byte
	ttl    time.Duration
}

func (s *Signer) message(method, key string, exp int64) string {
	return method + "\n" + key + "\n" + strconv.FormatInt(exp, 10)
}

func (s *Signer) sign(method, key string, exp int64) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(s.message(method, key, exp)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Presign 生成带过期时间与签名的直传 URL，方法被写进签名串不可篡改。
func (s *Signer) Presign(base, method, key string) (string, int64) {
	exp := time.Now().Add(s.ttl).Unix()
	q := url.Values{}
	q.Set("expires", strconv.FormatInt(exp, 10))
	q.Set("method", method)
	q.Set("signature", s.sign(method, key, exp))
	return base + "/" + key + "?" + q.Encode(), exp
}

// Verify 先判过期，再用常量时间比较签名，避免时序侧信道。
func (s *Signer) Verify(method, key string, exp int64, sig string) error {
	if time.Now().Unix() > exp {
		return fmt.Errorf("签名已过期")
	}
	want := s.sign(method, key, exp)
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return fmt.Errorf("签名不匹配")
	}
	return nil
}

func main() {
	s := &Signer{secret: []byte("test-secret-key"), ttl: 5 * time.Minute}
	method, key := "PUT", "sha256-abcdef"
	raw, exp := s.Presign("https://oss.example.com/upload", method, key)
	fmt.Println("预签名 URL 已生成, 过期时间戳=", exp)

	u, err := url.Parse(raw)
	if err != nil {
		fmt.Println("解析 URL 失败:", err)
		return
	}
	q := u.Query()
	gotExp, _ := strconv.ParseInt(q.Get("expires"), 10, 64)
	sig := q.Get("signature")
	fmt.Println("合法校验:", s.Verify(q.Get("method"), key, gotExp, sig))
	fmt.Println("篡改对象键:", s.Verify(q.Get("method"), key+"-evil", gotExp, sig))
	fmt.Println("过期签名:", s.Verify(q.Get("method"), key, time.Now().Add(-time.Hour).Unix(), sig))
	fmt.Println("篡改方法:", s.Verify("DELETE", key, gotExp, sig))
}

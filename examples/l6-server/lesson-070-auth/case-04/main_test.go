package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

var secret = []byte("test-secret")

func sign(head, payload string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(head + "." + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verify(token string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("malformed")
	}
	headBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return err
	}
	var head struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headBytes, &head); err != nil {
		return err
	}
	if head.Alg != "HS256" { // 固定算法白名单
		return fmt.Errorf("unexpected alg: %s", head.Alg)
	}
	if !hmac.Equal([]byte(sign(parts[0], parts[1])), []byte(parts[2])) {
		return fmt.Errorf("bad signature")
	}
	return nil
}

func TestPayloadIsEncodedNotEncrypted(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"sub": "u-1", "role": "admin"})
	payload := base64.RawURLEncoding.EncodeToString(body)
	decoded, _ := base64.RawURLEncoding.DecodeString(payload)
	if !strings.Contains(string(decoded), "admin") {
		t.Fatal("载荷应可明文读出")
	}
}

func TestAlgNoneIsRejected(t *testing.T) {
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u-1"}`))
	if err := verify(head + "." + body + "."); err == nil {
		t.Fatal("alg=none 竟然被接受了")
	}
}

func TestValidTokenPasses(t *testing.T) {
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u-1"}`))
	if err := verify(head + "." + body + "." + sign(head, body)); err != nil {
		t.Fatalf("合法令牌被拒: %v", err)
	}
}

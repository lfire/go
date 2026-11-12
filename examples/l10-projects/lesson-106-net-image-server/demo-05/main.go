package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

type contentStore struct {
	mu   sync.RWMutex
	data map[string][]byte
	typ  map[string]string
}

func newContentStore() *contentStore {
	return &contentStore{
		data: make(map[string][]byte),
		typ:  make(map[string]string),
	}
}

func (c *contentStore) Put(b []byte, ct string) string {
	sum := sha256.Sum256(b)
	key := hex.EncodeToString(sum[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[key]; !ok {
		c.data[key] = append([]byte(nil), b...)
		c.typ[key] = ct
	}
	return key
}

func (c *contentStore) Get(key string) ([]byte, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	b, ok := c.data[key]
	return b, c.typ[key], ok
}

func etagOf(key string) string {
	return "\"" + key + "\"" // entity-tag 必须带引号
}

// matchETag 兼容多值、弱校验与通配符，命中即回 304。
func matchETag(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		p := strings.TrimSpace(part)
		if p == etag || p == "W/"+etag || p == "*" {
			return true
		}
	}
	return false
}

func serveImage(c *contentStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/img/")
		b, ct, ok := c.Get(key)
		if !ok {
			http.NotFound(w, r)
			return
		}
		etag := etagOf(key)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("Content-Type", ct)
		if matchETag(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(b)
	})
}

func main() {
	c := newContentStore()
	key := c.Put([]byte("fake-image-bytes"), "image/png")
	srv := httptest.NewServer(serveImage(c))
	defer srv.Close()

	r1, _ := http.Get(srv.URL + "/img/" + key)
	body, _ := io.ReadAll(r1.Body)
	_ = r1.Body.Close()
	fmt.Println("首次:", r1.StatusCode, "bytes=", len(body), "ETag 有引号=",
		strings.HasPrefix(r1.Header.Get("ETag"), "\""))

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/img/"+key, nil)
	req.Header.Set("If-None-Match", r1.Header.Get("ETag"))
	r2, _ := http.DefaultClient.Do(req)
	b2, _ := io.ReadAll(r2.Body)
	_ = r2.Body.Close()
	fmt.Println("带缓存:", r2.StatusCode, "bytes=", len(b2))

	weak, _ := http.NewRequest(http.MethodGet, srv.URL+"/img/"+key, nil)
	weak.Header.Set("If-None-Match", "W/"+r1.Header.Get("ETag"))
	r3, _ := http.DefaultClient.Do(weak)
	_ = r3.Body.Close()
	fmt.Println("弱校验:", r3.StatusCode)
}

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const maxUpload = 8 << 20

type contentStore struct {
	mu   sync.RWMutex
	data map[string][]byte
	ct   map[string]string
}

func newContentStore() *contentStore {
	return &contentStore{data: make(map[string][]byte), ct: make(map[string]string)}
}

func (c *contentStore) PutKey(key string, b []byte, ct string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.data[key]; !ok {
		c.data[key] = append([]byte(nil), b...)
		c.ct[key] = ct
	}
}

func (c *contentStore) Get(key string) ([]byte, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	b, ok := c.data[key]
	return b, c.ct[key], ok
}

func sniff(head []byte) string {
	switch {
	case len(head) >= 3 && bytes.Equal(head[:3], []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case len(head) >= 8 && bytes.Equal(head[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "image/png"
	default:
		return ""
	}
}

// semaphore 用有缓冲 channel 限并发，超限立刻 503，不让请求堆积。
func semaphore(n int) func(http.Handler) http.Handler {
	sem := make(chan struct{}, n)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			default:
				http.Error(w, "服务繁忙", http.StatusServiceUnavailable)
			}
		})
	}
}

func uploadHandler(store *contentStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "只接受 POST", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "不是合法 multipart", http.StatusBadRequest)
			return
		}
		var part *multipart.Part
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, "读分片失败", http.StatusBadRequest)
				return
			}
			if p.FormName() == "image" {
				part = p
				break
			}
			p.Close()
		}
		if part == nil {
			http.Error(w, "缺少 image 字段", http.StatusBadRequest)
			return
		}
		defer part.Close()

		dir, err := os.MkdirTemp("", "img-")
		if err != nil {
			http.Error(w, "建临时目录失败", http.StatusInternalServerError)
			return
		}
		defer os.RemoveAll(dir) // 请求结束一定清理，杜绝临时文件泄漏

		tmp, err := os.CreateTemp(dir, "up-*")
		if err != nil {
			http.Error(w, "建临时文件失败", http.StatusInternalServerError)
			return
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(part, head)
		head = head[:n]
		ct := sniff(head)
		if ct == "" {
			tmp.Close()
			http.Error(w, "不是支持格式", http.StatusUnsupportedMediaType)
			return
		}
		h := sha256.New()
		_, _ = tmp.Write(head)
		_, _ = h.Write(head)
		if _, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(part, maxUpload)); err != nil {
			tmp.Close()
			http.Error(w, "流式读取失败", http.StatusBadRequest)
			return
		}
		if err := tmp.Close(); err != nil {
			http.Error(w, "关闭临时文件失败", http.StatusInternalServerError)
			return
		}
		data, err := os.ReadFile(tmp.Name())
		if err != nil {
			http.Error(w, "回读失败", http.StatusInternalServerError)
			return
		}
		key := hex.EncodeToString(h.Sum(nil))
		store.PutKey(key, data, ct)
		slog.Info("upload", "key", key[:12], "bytes", len(data), "ct", ct)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"key": key})
	})
}

func downloadHandler(store *contentStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/img/")
		b, ct, ok := store.Get(key)
		if !ok {
			http.NotFound(w, r)
			return
		}
		etag := "\"" + key + "\""
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("Content-Type", ct)
		if r.Header.Get("If-None-Match") == etag {
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
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	store := newContentStore()
	mux := http.NewServeMux()
	mux.Handle("/upload", semaphore(4)(uploadHandler(store)))
	mux.Handle("/img/", downloadHandler(store))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("监听失败:", err)
		return
	}
	go func() { _ = srv.Serve(ln) }()
	base := "http://" + ln.Addr().String()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("image", "a.png")
	_, _ = fw.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3})
	_ = mw.Close()
	resp, err := http.Post(base+"/upload", mw.FormDataContentType(), &buf)
	if err != nil {
		fmt.Println("上传请求失败:", err)
		return
	}
	var out struct {
		Key string `json:"key"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()
	fmt.Println("上传状态:", resp.StatusCode)

	r2, _ := http.Get(base + "/img/" + out.Key)
	_, _ = io.Copy(io.Discard, r2.Body)
	_ = r2.Body.Close()
	fmt.Println("首次下载:", r2.StatusCode, "ETag 存在=", r2.Header.Get("ETag") != "")

	req, _ := http.NewRequest(http.MethodGet, base+"/img/"+out.Key, nil)
	req.Header.Set("If-None-Match", r2.Header.Get("ETag"))
	r3, _ := http.DefaultClient.Do(req)
	_, _ = io.Copy(io.Discard, r3.Body)
	_ = r3.Body.Close()
	fmt.Println("带缓存下载:", r3.StatusCode)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Println("关闭超时:", err)
	}
	fmt.Println("服务已优雅退出")
}

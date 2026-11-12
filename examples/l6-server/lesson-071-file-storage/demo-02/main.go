package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const chunkSize = 1 << 20 // 每片 1 MiB

type Manager struct {
	mu       sync.Mutex
	dir      string
	hashes   map[string]string        // 内容哈希 -> 最终路径（秒传表）
	received map[string]map[int]bool  // uploadID -> 已到分片索引
	total    map[string]int           // uploadID -> 应有分片数
}

func NewManager(dir string) *Manager {
	_ = os.MkdirAll(dir, 0755)
	return &Manager{
		dir: dir, hashes: map[string]string{},
		received: map[string]map[int]bool{}, total: map[string]int{},
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *Manager) init(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	total, _ := strconv.Atoi(r.URL.Query().Get("total"))
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.hashes[hash]; ok {
		writeJSON(w, http.StatusOK,
			map[string]any{"instant": true, "path": p})
		return
	}
	id := newID()
	m.received[id] = map[int]bool{}
	m.total[id] = total
	writeJSON(w, http.StatusOK,
		map[string]any{"upload_id": id, "chunk_size": chunkSize})
}

func (m *Manager) chunk(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("upload_id")
	idx, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || idx < 0 {
		http.Error(w, "bad index", http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, chunkSize+1))
	if err != nil {
		http.Error(w, "read", http.StatusInternalServerError)
		return
	}
	part := filepath.Join(m.dir, id+".part")
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		http.Error(w, "open", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	// WriteAt 让断点重传同一分片成为幂等操作，且允许乱序到达。
	if _, err := f.WriteAt(data, int64(idx)*chunkSize); err != nil {
		http.Error(w, "write", http.StatusInternalServerError)
		return
	}
	m.mu.Lock()
	if m.received[id] == nil {
		m.mu.Unlock()
		http.Error(w, "unknown upload", http.StatusNotFound)
		return
	}
	m.received[id][idx] = true
	m.mu.Unlock()
	writeJSON(w, http.StatusOK,
		map[string]any{"index": idx, "size": len(data)})
}

func (m *Manager) complete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("upload_id")
	hash := r.URL.Query().Get("hash")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.received[id] == nil {
		http.Error(w, "unknown upload", http.StatusNotFound)
		return
	}
	if len(m.received[id]) < m.total[id] {
		writeJSON(w, http.StatusConflict, map[string]any{
			"received": len(m.received[id]), "total": m.total[id],
		})
		return
	}
	final := filepath.Join(m.dir, hash+".bin")
	if err := os.Rename(
		filepath.Join(m.dir, id+".part"), final); err != nil {
		http.Error(w, "rename", http.StatusInternalServerError)
		return
	}
	m.hashes[hash] = final // 之后同哈希文件可秒传
	delete(m.received, id)
	delete(m.total, id)
	writeJSON(w, http.StatusOK, map[string]any{"path": final})
}

func main() {
	m := NewManager("./uploads")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload/init", m.init)
	mux.HandleFunc("PUT /upload/chunk", m.chunk)
	mux.HandleFunc("POST /upload/complete", m.complete)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

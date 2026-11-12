package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
)

const maxUpload = 32 << 20 // 请求体上限 32 MiB

func upload(w http.ResponseWriter, r *http.Request) {
	// 先限总大小，避免恶意大 body 打满内存或磁盘。
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	// 内存中最多留 8 MiB，其余由 multipart 落到系统临时文件。
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "body too large or invalid", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll() // 清理 multipart 自己产生的临时文件

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "upload-*")
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	// 无论成功失败都删临时文件；不要把 defer 放进循环里攒到最后。
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	h := sha256.New()
	// MultiWriter 一次写两处：落盘的同时算哈希，不必读第二遍。
	written, err := io.Copy(io.MultiWriter(tmp, h), file)
	if err != nil {
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": header.Filename, "size": written,
		"sha256": hex.EncodeToString(h.Sum(nil)),
	})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload", upload)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

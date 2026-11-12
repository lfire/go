package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
)

const maxUpload = 8 << 20 // 8 MiB

// sniff 用魔数判断真实图片类型，不信客户端声明的 Content-Type。
func sniff(head []byte) string {
	switch {
	case len(head) >= 3 && bytes.Equal(head[:3], []byte{0xFF, 0xD8, 0xFF}):
		return "jpeg"
	case len(head) >= 8 && bytes.Equal(head[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "png"
	default:
		return ""
	}
}

// streamUpload 把分片流式落到临时文件，边写边算哈希，内存与文件大小无关。
func streamUpload(part *multipart.Part, dir string) (string, string, string, error) {
	head := make([]byte, 512)
	n, readErr := io.ReadFull(part, head)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", "", "", readErr
	}
	head = head[:n]
	kind := sniff(head)
	if kind == "" {
		return "", "", "", errors.New("不是支持的图片格式")
	}
	f, err := os.CreateTemp(dir, "up-*")
	if err != nil {
		return "", "", "", err
	}
	path := f.Name()
	h := sha256.New()
	if _, err := f.Write(head); err != nil {
		f.Close()
		return path, "", "", err
	}
	_, _ = h.Write(head)
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(part, maxUpload)); err != nil {
		f.Close()
		return path, "", "", err
	}
	if err := f.Close(); err != nil {
		return path, "", "", err
	}
	return path, kind, hex.EncodeToString(h.Sum(nil)), nil
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只接受 POST", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "不是合法的 multipart 请求", http.StatusBadRequest)
		return
	}
	var part *multipart.Part
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "读取分片失败", http.StatusBadRequest)
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
	path, kind, sum, err := streamUpload(part, dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil {
		http.Error(w, "读取临时文件失败", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "type=%s bytes=%d sha256=%s", kind, info.Size(), sum[:12])
}

func main() {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("image", "a.png")
	_, _ = fw.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4})
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	uploadHandler(rec, req)
	fmt.Println("合法上传:", rec.Code, rec.Body.String())

	var bad bytes.Buffer
	mw2 := multipart.NewWriter(&bad)
	fw2, _ := mw2.CreateFormFile("image", "evil.png")
	_, _ = fw2.Write([]byte("<html>not an image</html>"))
	_ = mw2.Close()
	req2 := httptest.NewRequest(http.MethodPost, "/upload", &bad)
	req2.Header.Set("Content-Type", mw2.FormDataContentType())
	rec2 := httptest.NewRecorder()
	uploadHandler(rec2, req2)
	fmt.Println("伪装文件:", rec2.Code, "临时目录已由 defer 清理")
}

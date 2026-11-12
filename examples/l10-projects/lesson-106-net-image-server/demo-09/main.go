package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

type Metrics struct {
	UploadBytes atomic.Int64
	BombReject  atomic.Int64
	NotModified atomic.Int64
	ThumbFail   atomic.Int64
}

func (m *Metrics) ObserveUpload(bytes int64, status int) {
	m.UploadBytes.Add(bytes)
	slog.Info("upload", "bytes", bytes, "status", status)
}

func (m *Metrics) ObserveReject(reason string) {
	if reason == "pixel-bomb" {
		m.BombReject.Add(1)
	}
	slog.Warn("reject", "reason", reason)
}

func (m *Metrics) Ratio304(total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(m.NotModified.Load()) / float64(total)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	m := &Metrics{}
	m.ObserveUpload(2<<20, 201)
	m.ObserveReject("pixel-bomb")
	m.ObserveReject("bad-magic")
	m.NotModified.Add(85)
	fmt.Println("累计上传字节:", m.UploadBytes.Load(),
		"像素炸弹拒绝:", m.BombReject.Load(),
		"304 占比:", m.Ratio304(100))
}

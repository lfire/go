package main

import (
	"bytes"
	"fmt"
	"log/slog"
)

func main() {
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo} {
		var buf bytes.Buffer
		h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})
		log := slog.New(h)
		for i := 0; i < 1000; i++ {
			log.Debug("高频事件", "i", i)
			log.Info("关键事件", "i", i)
		}
		lines := bytes.Count(buf.Bytes(), []byte{'\n'})
		fmt.Printf("门槛=%s 输出行数=%d\n", level, lines)
	}
}

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"time"
)

// span 记录一个阶段的耗时与关键字段，defer 一行即可埋点。
func span(stage, inputSHA, version string, start time.Time, skipped, changed int) {
	slog.Info("gencode_stage",
		slog.String("stage", stage),
		slog.String("input_sha", inputSHA),
		slog.String("generator_version", version),
		slog.Int("duration_ms", int(time.Since(start).Milliseconds())),
		slog.Int("skipped", skipped),
		slog.Int("changed", changed),
		slog.String("run_id", os.Getenv("CI_JOB_ID")),
	)
}

func fingerprint(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])[:8]
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	src := []byte("package errs\nconst ErrNotFound = 404\n")
	sha := fingerprint(src)
	start := time.Now()
	// 这里代表真实的解析与渲染工作。
	time.Sleep(3 * time.Millisecond)
	span("render", sha, "v1.4.0", start, 0, 1)
	slog.Warn("检测到跳过节点，请复核输入",
		slog.String("input_sha", sha), slog.Int("skipped", 1))
}

// 级别过滤与脱敏：Debug 默认不输出，敏感字段先打码再记录
package main

import (
	"log/slog"
	"os"
	"strings"
)

// mask 只保留手机号后四位，避免明文落盘
func mask(phone string) string {
	if len(phone) < 4 {
		return "****"
	}
	return "****" + phone[len(phone)-4:]
}

func main() {
	// LevelInfo 会过滤掉 Debug，直接减少高频日志量
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	log := slog.New(h)
	log.Debug("enter_func", "step", 1) // 不会输出
	log.Info("user_registered", "phone", mask("13800138000"))
	log.Info("pay_callback", "note", strings.ToUpper("ok"))
}

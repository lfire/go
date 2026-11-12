// 结构化日志：字段是键值对，可被检索与聚合
package main

import (
	"log/slog"
	"os"
)

func main() {
	// NewJSONHandler 每行输出一个 JSON 对象，便于日志系统解析
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	slog.Info("订单创建成功",
		"order_id", "A-1001",
		"amount", 280,
		"user_id", 7,
	)
	slog.Warn("库存偏低", "sku", "SKU-9", "left", 3)
}

// internal/order/model.go 的等价可运行示例：三种数据对象各司其职
package main

import (
	"encoding/json"
	"fmt"
)

// OrderDO 面向存储：保留数据库字段与内部状态码。
type OrderDO struct {
	ID        int64 `json:"id"`
	Status    int   `json:"status"`
	CreatedAt int64 `json:"created_at"`
}

// OrderDTO 面向服务间传输：字段名跟随接口契约。
type OrderDTO struct {
	OrderID string `json:"order_id"`
	Amount  int    `json:"amount"`
}

// OrderVO 面向前端展示：带展示文案，金额已格式化。
type OrderVO struct {
	Title      string `json:"title"`
	AmountText string `json:"amount_text"`
}

func main() {
	do := OrderDO{ID: 1001, Status: 2, CreatedAt: 1700000000}
	dto := OrderDTO{OrderID: "A-1001", Amount: 280}
	vo := OrderVO{Title: "订单 A-1001", AmountText: "￥280.00"}
	for _, v := range []any{do, dto, vo} {
		b, err := json.Marshal(v)
		if err != nil {
			fmt.Println("序列化失败:", err)
			continue
		}
		fmt.Println(string(b))
	}
}

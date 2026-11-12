package main

import (
	"fmt"
	"sync"
)

// Outbox 模拟本地消息表：业务数据与消息在同一事务里写入
type Outbox struct {
	mu      sync.Mutex
	records []record
}

type record struct {
	biz    string
	status string // pending / sent
}

// CommitBizAndMessage 用一次本地事务同时落业务数据和待发消息
func (o *Outbox) CommitBizAndMessage(biz string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records = append(o.records, record{biz: biz, status: "pending"})
	fmt.Println("本地事务提交:", biz, "+ 消息 pending")
}

// Deliver 后台任务扫描未发送消息并投递，投递成功才标记 sent
func (o *Outbox) Deliver(consumer map[string]bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := range o.records {
		r := &o.records[i]
		if r.status != "pending" {
			continue
		}
		// 消费端按 biz 幂等：重复投递也只生效一次
		if !consumer[r.biz] {
			consumer[r.biz] = true
			fmt.Println("投递并消费:", r.biz)
		} else {
			fmt.Println("重复投递被幂等挡住:", r.biz)
		}
		r.status = "sent"
	}
}

func main() {
	o := &Outbox{}
	consumer := make(map[string]bool)
	o.CommitBizAndMessage("扣款:order-1")
	o.CommitBizAndMessage("扣款:order-2")

	o.Deliver(consumer) // 正常投递
	// 模拟投递确认丢失、定时任务把同一条消息重新置为待发
	o.records[0].status = "pending"
	o.Deliver(consumer) // 重复投递，靠幂等消费兜底
}

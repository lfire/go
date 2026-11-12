package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/segmentio/kafka-go"
)

const (
	broker = "127.0.0.1:9092"
	topic  = "orders"
	group  = "order-service"
)

func newWriter() *kafka.Writer {
	return &kafka.Writer{
		Addr:         kafka.TCP(broker),
		Topic:        topic,
		Balancer:     &kafka.Hash{},     // 按 Key 哈希到固定分区，保证同键有序
		RequiredAcks: kafka.RequireAll,  // 所有 ISR 副本确认才算成功
		Async:        false,             // 同步写，失败能立刻返回给调用方
	}
}

func newReader() *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{broker},
		Topic:          topic,
		GroupID:        group,
		MinBytes:       1,
		MaxBytes:       10 << 20,
		CommitInterval: 0, // 关闭自动提交，改为处理成功后手动提交
	})
}

func consume(ctx context.Context, r *kafka.Reader) {
	for {
		m, err := r.FetchMessage(ctx)
		if err != nil {
			log.Printf("fetch: %v", err)
			return
		}
		log.Printf("消费 partition=%d offset=%d value=%s",
			m.Partition, m.Offset, m.Value)
		// 业务处理成功后才提交；崩溃则 offset 不前进，消息会被重投。
		if err := r.CommitMessages(ctx, m); err != nil {
			log.Printf("commit: %v", err)
		}
	}
}

func main() {
	ctx := context.Background()
	w := newWriter()
	defer w.Close()
	r := newReader()
	defer r.Close()
	go consume(ctx, r)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", func(wr http.ResponseWriter, req *http.Request) {
		var body struct {
			OrderID string `json:"order_id"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(wr, "bad json", http.StatusBadRequest)
			return
		}
		// 订单号作 Key：同一订单恒定落同一分区，分区内保持顺序。
		msg := kafka.Message{
			Key:   []byte(body.OrderID),
			Value: []byte(body.OrderID),
		}
		if err := w.WriteMessages(req.Context(), msg); err != nil {
			http.Error(wr, "publish failed", http.StatusBadGateway)
			return
		}
		wr.WriteHeader(http.StatusAccepted) // 202：已接收，尚未处理完
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

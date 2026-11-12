package main

import (
	"fmt"
	"time"
)

type Doc struct {
	ID      string
	Title   string
	Version int
}

type SearchStore struct {
	docs map[string]Doc
}

// Upsert 只接受版本号更新的写入，乱序到达的旧事件不会覆盖新数据。
func (s *SearchStore) Upsert(d Doc) bool {
	old, ok := s.docs[d.ID]
	if ok && old.Version >= d.Version {
		fmt.Println("丢弃乱序或重复事件:", d.ID, "version", d.Version)
		return false
	}
	s.docs[d.ID] = d
	fmt.Println("写入索引:", d.ID, d.Title, "version", d.Version)
	return true
}

type ChangeEvent struct {
	Doc     Doc
	Retries int
}

type Syncer struct {
	es       *SearchStore
	events   chan ChangeEvent
	maxRetry int
	failOnce bool
}

func (s *Syncer) Run(done chan struct{}) {
	for e := range s.events {
		if err := s.apply(e); err != nil {
			e.Retries++
			if e.Retries > s.maxRetry {
				fmt.Println("同步失败需人工介入:", e.Doc.ID)
				continue
			}
			time.Sleep(10 * time.Millisecond) // 生产用指数退避
			s.events <- e
			continue
		}
	}
	close(done)
}

func (s *Syncer) apply(e ChangeEvent) error {
	if s.failOnce {
		s.failOnce = false
		return fmt.Errorf("temporary failure") // 模拟一次网络抖动
	}
	s.es.Upsert(e.Doc)
	return nil
}

func main() {
	store := &SearchStore{docs: make(map[string]Doc)}
	s := &Syncer{
		es: store, events: make(chan ChangeEvent, 16),
		maxRetry: 3, failOnce: true,
	}
	done := make(chan struct{})
	go s.Run(done)

	// 模拟从 binlog / MQ 收到的变更，顺序可能因重投而打乱。
	s.events <- ChangeEvent{Doc: Doc{ID: "1", Title: "v1", Version: 1}}
	s.events <- ChangeEvent{Doc: Doc{ID: "1", Title: "v3", Version: 3}}
	s.events <- ChangeEvent{Doc: Doc{ID: "1", Title: "v2", Version: 2}}
	time.Sleep(50 * time.Millisecond)
	close(s.events)
	<-done
	fmt.Println("最终文档:", store.docs["1"].Title,
		"version", store.docs["1"].Version)
}

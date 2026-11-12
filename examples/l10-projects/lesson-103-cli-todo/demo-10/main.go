package main

import (
	"fmt"
	"time"
)

type Task struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}

// archiveTasks 返回归档条数与保留下来的任务；正好等于 ttl 的不归档。
func archiveTasks(all []Task, ttl time.Duration, now time.Time) (int, []Task) {
	kept := make([]Task, 0, len(all))
	moved := 0
	for _, t := range all {
		if t.Done && now.Sub(t.CreatedAt) > ttl {
			moved++
			continue
		}
		kept = append(kept, t)
	}
	return moved, kept
}

func main() {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	all := []Task{
		{ID: 1, Done: true, CreatedAt: now.Add(-31 * 24 * time.Hour)},
		{ID: 2, Done: true, CreatedAt: now.Add(-30 * 24 * time.Hour)},
		{ID: 3, Done: false, CreatedAt: now.Add(-40 * 24 * time.Hour)},
	}
	n, kept := archiveTasks(all, 30*24*time.Hour, now)
	fmt.Printf("归档=%d 保留=%d\n", n, len(kept))
}

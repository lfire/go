package main

import (
	"fmt"
	"time"
)

type Task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// Store 是领域层定义的存储契约，cmd 与编排器只依赖它。
type Store interface {
	List() ([]Task, error)
	Add(title string) (Task, error)
	Done(id int) (Task, error)
	Archive(ttl time.Duration) (int, error)
}

type memStore struct{ tasks []Task }

func (m *memStore) List() ([]Task, error) { return m.tasks, nil }

func (m *memStore) Add(t string) (Task, error) {
	max := 0
	for _, x := range m.tasks {
		if x.ID > max {
			max = x.ID
		}
	}
	x := Task{ID: max + 1, Title: t}
	m.tasks = append(m.tasks, x)
	return x, nil
}

func (m *memStore) Done(id int) (Task, error) { return m.tasks[id-1], nil }
func (m *memStore) Archive(time.Duration) (int, error) { return 0, nil }

func main() {
	var s Store = &memStore{}
	s.Add("示例任务")
	all, _ := s.List()
	fmt.Println("任务数:", len(all))
}

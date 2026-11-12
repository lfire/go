package main

import (
	"context"
	"fmt"
	"sync"
)

type Result struct {
	Index   int
	Name    string
	Content []byte
	Err     error
}

// renderOne 是纯函数：同样的输入永远得到同样的字节。
func renderOne(name string, code int) ([]byte, error) {
	if code < 0 {
		return nil, fmt.Errorf("非法错误码 %d", code)
	}
	return []byte(fmt.Sprintf("// %s\nconst C = %d\n", name, code)), nil
}

// RunPool 并发渲染所有文件，结果按输入顺序放回，保证产物确定性。
func RunPool(ctx context.Context, jobs []struct {
	Name string
	Code int
}) ([]Result, error) {
	type job struct {
		idx  int
		item struct {
			Name string
			Code int
		}
	}
	workers := 4
	tasks := make(chan job)
	results := make(chan Result, len(jobs))

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				select {
				case <-ctx.Done():
					results <- Result{Index: t.idx, Err: ctx.Err()}
					return
				default:
				}
				b, err := renderOne(t.item.Name, t.item.Code)
				results <- Result{Index: t.idx, Name: t.item.Name, Content: b, Err: err}
			}
		}()
	}
	for i, j := range jobs {
		tasks <- job{idx: i, item: j}
	}
	close(tasks)
	go func() { wg.Wait(); close(results) }()

	out := make([]Result, len(jobs))
	var firstErr error
	for r := range results {
		out[r.Index] = r
		if r.Err != nil && firstErr == nil {
			firstErr = r.Err
		}
	}
	return out, firstErr
}

func main() {
	jobs := []struct {
		Name string
		Code int
	}{
		{Name: "order", Code: 20404},
		{Name: "payment", Code: 30409},
		{Name: "inventory", Code: 40429},
		{Name: "gateway", Code: 50503},
	}
	results, err := RunPool(context.Background(), jobs)
	fmt.Println("首个错误:", err)
	for _, r := range results {
		fmt.Printf("index=%d name=%s bytes=%d\n", r.Index, r.Name, len(r.Content))
	}
}

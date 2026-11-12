package main

import (
	"context"
	"fmt"
	"sync"
)

// Page 是下载产物，Parser 与 Store 都只认这个结构。
type Page struct {
	URL   string
	Title string
}

type Parser interface {
	Parse(url string, body []byte) Page
}
type Store interface {
	Save(ctx context.Context, p Page) error
}

type titleParser struct{}

func (titleParser) Parse(url string, body []byte) Page {
	return Page{URL: url, Title: fmt.Sprintf("title-%d", len(body))}
}

type memStore struct {
	mu   sync.Mutex
	rows []Page
}

func (s *memStore) Save(ctx context.Context, p Page) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, p) // 幂等由上层唯一键保证
	return nil
}

// Pipeline 把下载、解析、存储用 channel 串起来，任意一段可替换。
func Pipeline(ctx context.Context, urls []string, n int, p Parser, st Store) error {
	type raw struct {
		url  string
		body []byte
	}
	downloads := make(chan raw, n)
	pages := make(chan Page, n)
	var wg sync.WaitGroup

	go func() {
		defer close(downloads)
		for _, u := range urls {
			select {
			case downloads <- raw{url: u, body: []byte("xxxxxxxxxx")}:
			case <-ctx.Done():
				return
			}
		}
	}()

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range downloads {
				select {
				case pages <- p.Parse(d.url, d.body):
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(pages)
	}()

	for pg := range pages {
		if err := st.Save(ctx, pg); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	st := &memStore{}
	urls := []string{"/a", "/b", "/c", "/d", "/e"}
	if err := Pipeline(context.Background(), urls, 2, titleParser{}, st); err != nil {
		fmt.Println("pipeline 失败:", err)
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	fmt.Println("落库条数:", len(st.rows), "首条:", st.rows[0].URL, st.rows[0].Title)
}

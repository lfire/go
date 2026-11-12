package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
)

// Task 是抓取任务的最小契约：规范化 URL 与抓取深度。
type Task struct {
	URL   string
	Depth int
}

// ParseResult 是解析器输出：本页标题与抽出的后续链接。
type ParseResult struct {
	Title string
	Links []string
}

// Parser 把下载与解析解耦，抓取层只依赖这个接口。
type Parser interface {
	Parse(body []byte) ParseResult
}

type linkParser struct{ re *regexp.Regexp }

func newLinkParser() *linkParser {
	return &linkParser{re: regexp.MustCompile(`href="(/[a-zA-Z0-9/_-]*)"`)}
}

func (p *linkParser) Parse(body []byte) ParseResult {
	var out ParseResult
	out.Title = "page"
	for _, m := range p.re.FindAllSubmatch(body, -1) {
		out.Links = append(out.Links, string(m[1]))
	}
	return out
}

// CrawlSingle 是 V1 基线：单队列、单循环，一次只处理一个 URL。
func CrawlSingle(base string, client *http.Client, parser Parser, max int, maxBody int64) []string {
	seen := map[string]bool{}
	queue := []Task{{URL: "/"}}
	var visited []string
	for len(queue) > 0 && len(visited) < max {
		t := queue[0]
		queue = queue[1:]
		if seen[t.URL] {
			continue
		}
		seen[t.URL] = true
		resp, err := client.Get(base + t.URL)
		if err != nil || resp.StatusCode != http.StatusOK {
			continue // 单线程版：一页失败就跳过，不影响后续
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		resp.Body.Close()
		visited = append(visited, t.URL)
		for _, link := range parser.Parse(body).Links {
			if !seen[link] {
				queue = append(queue, Task{URL: link, Depth: t.Depth + 1})
			}
		}
	}
	return visited
}

func main() {
	pages := map[string]string{
		"/":  `<a href="/a">a</a><a href="/b">b</a>`,
		"/a": `<a href="/d">d</a>`,
		"/b": `<a href="/d">d</a>`,
		"/d": `end`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := pages[r.URL.Path]; ok {
			fmt.Fprint(w, body)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	visited := CrawlSingle(srv.URL, srv.Client(), newLinkParser(), 10, 1<<20)
	fmt.Println("V1 抓取集合:", strings.Join(visited, " "), "总数", len(visited))
}

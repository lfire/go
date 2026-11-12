package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

var es *elasticsearch.Client

func main() {
	var err error
	es, err = elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{"http://127.0.0.1:9200"},
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := setup(context.Background(), es); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /search", searchHandler)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// setup 建索引并批量灌入演示数据，只在启动时执行一次。
func setup(ctx context.Context, es *elasticsearch.Client) error {
	// mapping 一旦写入就不能随意改字段类型，改结构通常需要 reindex。
	mapping := `{
		"mappings": {
			"properties": {
				"title":  { "type": "text", "analyzer": "standard" },
				"status": { "type": "keyword" },
				"price":  { "type": "integer" }
			}
		}
	}`
	req := esapi.IndicesCreateRequest{
		Index: "articles", Body: strings.NewReader(mapping),
	}
	res, err := req.Do(ctx, es)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	log.Println("create index status:", res.Status())

	// Bulk 写入：每条文档前一行是 action 元数据，后一行是文档内容。
	var buf bytes.Buffer
	docs := []map[string]any{
		{"title": "Go net/http deep dive", "status": "published", "price": 30},
		{"title": "Go redis cache guide", "status": "published", "price": 20},
	}
	for i, d := range docs {
		meta := map[string]any{
			"index": map[string]any{"_id": fmt.Sprint(i + 1)},
		}
		_ = json.NewEncoder(&buf).Encode(meta)
		_ = json.NewEncoder(&buf).Encode(d)
	}
	bulkRes, err := es.Bulk(bytes.NewReader(buf.Bytes()),
		es.Bulk.WithIndex("articles"),
		es.Bulk.WithRefresh("true")) // 演示用：立即刷新，生产别逐条刷新
	if err != nil {
		return err
	}
	defer bulkRes.Body.Close()
	log.Println("bulk status:", bulkRes.Status())
	return nil
}

// buildQuery 用结构体序列化出 DSL，绝不手工拼接用户输入的字符串。
func buildQuery(q string) string {
	body := map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"must": []any{
					map[string]any{"match": map[string]any{"title": q}},
				},
				// filter 不参与打分且可缓存，适合状态这类精确条件。
				"filter": []any{
					map[string]any{"term": map[string]any{"status": "published"}},
				},
			},
		},
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	res, err := es.Search(
		es.Search.WithContext(r.Context()),
		es.Search.WithIndex("articles"),
		es.Search.WithBody(strings.NewReader(buildQuery(q))),
	)
	if err != nil {
		http.Error(w, "search failed", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()

	var out struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Hits []struct {
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		http.Error(w, "decode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"total": out.Hits.Total.Value,
		"docs":  out.Hits.Hits,
	})
}

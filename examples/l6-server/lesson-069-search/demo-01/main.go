package main

import (
	"fmt"
	"sort"
	"strings"
)

type Index struct {
	postings map[string][]int // 词项 -> 文档 ID 列表（天然有序）
	docs     []string
}

func NewIndex() *Index {
	return &Index{postings: make(map[string][]int)}
}

// tokenize 是最朴素的分词：转小写后按空白切分。
// 中文生产环境需要 IK 等分词器，这里用空格分隔的词演示原理。
func tokenize(s string) []string {
	return strings.Fields(strings.ToLower(s))
}

func (idx *Index) Add(doc string) int {
	id := len(idx.docs)
	idx.docs = append(idx.docs, doc)
	for _, term := range tokenize(doc) {
		list := idx.postings[term]
		// 同一文档里重复出现的词只记一次。
		if len(list) == 0 || list[len(list)-1] != id {
			idx.postings[term] = append(list, id)
		}
	}
	return id
}

// Search 对多个词项的倒排列表取交集，实现 AND 语义。
func (idx *Index) Search(query string) []string {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	// 先处理最短的列表，让交集尽快收缩，减少比较次数。
	sort.Slice(terms, func(i, j int) bool {
		return len(idx.postings[terms[i]]) < len(idx.postings[terms[j]])
	})
	hits := idx.postings[terms[0]]
	for _, term := range terms[1:] {
		hits = intersect(hits, idx.postings[term])
		if len(hits) == 0 {
			break
		}
	}
	out := make([]string, 0, len(hits))
	for _, id := range hits {
		out = append(out, idx.docs[id])
	}
	return out
}

// intersect 求两个有序 ID 列表的交集，双指针线性扫描。
func intersect(a, b []int) []int {
	out := make([]int, 0)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			out = append(out, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return out
}

func main() {
	idx := NewIndex()
	idx.Add("go web server with net/http")
	idx.Add("go redis cache for web api")
	idx.Add("elasticsearch inverted index for search")

	fmt.Println("查询 go web:")
	for _, d := range idx.Search("go web") {
		fmt.Println(" -", d)
	}
	fmt.Println("查询 go redis:")
	for _, d := range idx.Search("go redis") {
		fmt.Println(" -", d)
	}
	fmt.Println("倒排表大小:", len(idx.postings))
}

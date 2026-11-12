package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

type Row map[string]string
type Emit func(Row) error
type Pred func(Row) bool

// DataSource：Scan 全量流式，ScanFiltered 把谓词下推到扫描层。
type DataSource interface {
	Columns() []string
	Scan(Emit) error
	ScanFiltered(Pred, Emit) error
	SupportsPushdown() bool
}

// CSVSource 流式读取，一次只持有一行，内存与总行数无关。
type CSVSource struct {
	r    io.Reader
	head []string
}

func (c *CSVSource) Columns() []string      { return c.head }
func (c *CSVSource) SupportsPushdown() bool { return false }
func (c *CSVSource) Scan(emit Emit) error {
	cr := csv.NewReader(c.r)
	head, err := cr.Read()
	if err != nil {
		return fmt.Errorf("读表头: %w", err)
	}
	c.head = head
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读数据行: %w", err)
		}
		row := make(Row, len(head))
		for i, h := range head {
			if i < len(rec) {
				row[h] = rec[i]
			}
		}
		if err := emit(row); err != nil {
			return err
		}
	}
}
func (c *CSVSource) ScanFiltered(p Pred, emit Emit) error {
	return c.Scan(func(r Row) error {
		if !p(r) {
			return nil
		}
		return emit(r)
	})
}

// SQLSource 概念演示：支持下推时把 WHERE 交给数据库，上行行数大幅减少。
type SQLSource struct {
	rows []Row
}

func (s SQLSource) Columns() []string      { return []string{"status", "size"} }
func (s SQLSource) SupportsPushdown() bool { return true }
func (s SQLSource) Scan(emit Emit) error {
	for _, r := range s.rows {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}
func (s SQLSource) ScanFiltered(p Pred, emit Emit) error {
	for _, r := range s.rows {
		if !p(r) {
			continue
		}
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	src := &CSVSource{r: strings.NewReader("status,size\n200,2048\n500,64\n200,128\n")}
	n := 0
	err := src.Scan(func(r Row) error {
		if r["status"] == "200" {
			n++
		}
		return nil
	})
	fmt.Printf("列=%v 命中=%d 错误=%v 支持下推=%v\n", src.Columns(), n, err, src.SupportsPushdown())
}

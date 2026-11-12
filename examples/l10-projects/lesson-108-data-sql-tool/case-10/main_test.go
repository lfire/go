package sqltool

import (
	"strings"
	"testing"
)

type Row map[string]string
type Emit func(Row) error

// DataSource 是被测契约；新增实现必须能跑同一组用例。
type DataSource interface {
	Columns() []string
	Scan(Emit) error
}

type sliceSource struct {
	cols []string
	rows []Row
}

func (s sliceSource) Columns() []string { return s.cols }
func (s sliceSource) Scan(emit Emit) error {
	for _, r := range s.rows {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

type csvSource struct {
	text string
}

func (c csvSource) Columns() []string { return []string{"status", "size"} }
func (c csvSource) Scan(emit Emit) error {
	lines := strings.Split(strings.TrimSpace(c.text), "\n")
	for _, ln := range lines[1:] {
		fields := strings.Split(ln, ",")
		if len(fields) != 2 {
			continue
		}
		if err := emit(Row{"status": fields[0], "size": fields[1]}); err != nil {
			return err
		}
	}
	return nil
}

// TestSourcesSameContract 用同一份期望验证所有数据源实现，是接口抽象最直接的证明。
func TestSourcesSameContract(t *testing.T) {
	sources := map[string]DataSource{
		"slice": sliceSource{cols: []string{"status", "size"}, rows: []Row{
			{"status": "200", "size": "1"},
			{"status": "500", "size": "2"},
			{"status": "200", "size": "3"},
		}},
		"csv": csvSource{text: "status,size\n200,1\n500,2\n200,3\n"},
	}
	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			hit := 0
			err := src.Scan(func(r Row) error {
				if r["status"] == "200" {
					hit++
				}
				return nil
			})
			if err != nil {
				t.Fatalf("扫描失败: %v", err)
			}
			if hit != 2 {
				t.Fatalf("命中=%d, want 2", hit)
			}
		})
	}
}

func BenchmarkSliceSourceScan(b *testing.B) {
	src := sliceSource{cols: []string{"status"}, rows: []Row{{"status": "200"}}}
	for i := 0; i < b.N; i++ {
		_ = src.Scan(func(Row) error { return nil })
	}
}

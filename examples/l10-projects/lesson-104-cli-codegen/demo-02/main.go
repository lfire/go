package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
)

type ErrSpec struct {
	Name    string
	Code    int
	Message string
}

// ParseResult 除模型外带跳过计数，把静默降级变成可观测事件。
type ParseResult struct {
	Codes   []ErrSpec
	Skipped int
}

// ParseCodes 扫描 const 块，抽整数常量与紧邻的文档注释。
// 所有类型断言都用 comma-ok，绝不对意外节点做裸断言。
func ParseCodes(filename string, src any) (ParseResult, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return ParseResult{}, fmt.Errorf("解析 %s: %w", filename, err)
	}
	var res ParseResult
	ast.Inspect(f, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		doc := ""
		if gd.Doc != nil {
			doc = gd.Doc.Text()
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				res.Skipped++
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					res.Skipped++ // 隐式继承上一行，无字面值
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					res.Skipped++
					continue
				}
				v, err := strconv.Atoi(lit.Value)
				if err != nil {
					res.Skipped++
					continue
				}
				res.Codes = append(res.Codes, ErrSpec{
					Name: name.Name, Code: v, Message: doc,
				})
			}
		}
		return false // const 块已处理完，不再深入其子节点
	})
	return res, nil
}

func main() {
	src := `package errs
// ErrNotFound 资源不存在
const ErrNotFound = 404
// ErrRateLimit 触发限流
const ErrRateLimit = 429
const implicit = ErrRateLimit
const label = "not an int"
`
	res, err := ParseCodes("codes.go", src)
	if err != nil {
		fmt.Println("解析失败:", err)
		return
	}
	fmt.Printf("抽出 %d 项，跳过 %d 项\n", len(res.Codes), res.Skipped)
	for _, c := range res.Codes {
		fmt.Printf("%s = %d\n", c.Name, c.Code)
	}
}

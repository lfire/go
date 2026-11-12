package main

import (
	"errors"
	"fmt"
	"sort"
)

// ErrSpec 是解析层与渲染层之间唯一的中间模型。
// 模板只认识这几个字段，输入格式变化时只需改解析器。
type ErrSpec struct {
	Name    string // 常量名，如 ErrNotFound
	Code    int    // 数值错误码，全局唯一
	Message string // 面向用户的可读文案
	Service string // 归属服务，用于按服务分文件生成
}

var (
	ErrDuplicateCode = errors.New("错误码数值重复")
	ErrDuplicateName = errors.New("错误码名称重复")
	ErrEmptyName     = errors.New("错误码名称为空")
)

// Registry 是一份不可变的目标产物模型，Validate 在渲染前集中校验。
type Registry struct {
	Package string
	Codes   []ErrSpec
}

// Validate 做三件事：查空名、查重复数值、查重复名称。
// 返回第一个具体错误，避免生成出能编译但语义错误的注册表。
func (r Registry) Validate() error {
	byCode := make(map[int]string, len(r.Codes))
	byName := make(map[string]struct{}, len(r.Codes))
	for _, c := range r.Codes {
		if c.Name == "" {
			return fmt.Errorf("%w: 数值 %d 对应项", ErrEmptyName, c.Code)
		}
		if prev, ok := byCode[c.Code]; ok {
			return fmt.Errorf("%w: %d 被 %s 与 %s 同时使用",
				ErrDuplicateCode, c.Code, prev, c.Name)
		}
		if _, ok := byName[c.Name]; ok {
			return fmt.Errorf("%w: %s", ErrDuplicateName, c.Name)
		}
		byCode[c.Code] = c.Name
		byName[c.Name] = struct{}{}
	}
	return nil
}

// Sorted 返回按数值排序的副本，保证多次生成字节完全稳定。
func (r Registry) Sorted() []ErrSpec {
	out := append([]ErrSpec(nil), r.Codes...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Code < out[j].Code
	})
	return out
}

func main() {
	r := Registry{Package: "errs", Codes: []ErrSpec{
		{Name: "ErrNotFound", Code: 404, Message: "资源不存在", Service: "order"},
		{Name: "ErrRateLimit", Code: 429, Message: "请求过于频繁", Service: "order"},
	}}
	if err := r.Validate(); err != nil {
		fmt.Println("校验失败:", err)
		return
	}
	for _, c := range r.Sorted() {
		fmt.Printf("%d %s %s\n", c.Code, c.Name, c.Message)
	}
	bad := Registry{Codes: []ErrSpec{
		{Name: "ErrA", Code: 404}, {Name: "ErrB", Code: 404},
	}}
	fmt.Println("冲突检测:", bad.Validate())
}

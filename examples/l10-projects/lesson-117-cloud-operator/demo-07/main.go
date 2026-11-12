// 文件：internal/reconcile/compat.go
package main

import (
	"fmt"
	"sort"
	"strings"
)

// SpecV1Alpha1 是老版本：replicas 是指针（可缺省），没有 strategy 字段。
type SpecV1Alpha1 struct {
	Replicas *int
	Version  string
	Image    string
}

// SpecV1Beta1 是新版本：replicas 变成必填值，新增 strategy 字段。
type SpecV1Beta1 struct {
	Replicas int
	Version  string
	Image    string
	Strategy string
}

// Convert 把老对象升级为新对象，缺省字段在此补齐，保证反序列化安全。
func Convert(in SpecV1Alpha1) (SpecV1Beta1, error) {
	out := SpecV1Beta1{
		Version:  in.Version,
		Image:    in.Image,
		Strategy: "RollingUpdate",
	}
	if in.Replicas == nil {
		out.Replicas = 1 // 老对象没写副本数，按缺省 1 处理
	} else {
		out.Replicas = *in.Replicas
	}
	if out.Replicas < 1 {
		return SpecV1Beta1{}, fmt.Errorf("invalid replicas: %d", out.Replicas)
	}
	if !strings.HasPrefix(out.Version, "v") {
		return SpecV1Beta1{}, fmt.Errorf("invalid version: %s", out.Version)
	}
	return out, nil
}

// Rule 是 RBAC 的一条授权，LeastPrivilege 校验只放行必要动作。
type Rule struct {
	Verbs    []string
	Resource string
}

func CheckLeastPrivilege(rules []Rule) []string {
	var bad []string
	for _, r := range rules {
		for _, v := range r.Verbs {
			if v == "*" || v == "delete" || v == "deletecollection" {
				bad = append(bad, r.Resource+" verb="+v)
			}
		}
		if strings.Contains(r.Resource, "secrets") {
			bad = append(bad, r.Resource+" is sensitive")
		}
	}
	sort.Strings(bad)
	return bad
}

func main() {
	old := SpecV1Alpha1{Version: "v1.4.0", Image: "kv:v1.4.0"} // 没写 replicas
	converted, err := Convert(old)
	fmt.Println("converted:", converted.Replicas, converted.Strategy, "err:", err)
	rules := []Rule{
		{Verbs: []string{"get", "list", "watch", "patch", "update"}, Resource: "kvapps"},
		{Verbs: []string{"delete"}, Resource: "deployments"}, // 违规：不该给删除权
		{Verbs: []string{"get"}, Resource: "secrets"},        // 违规：敏感资源
	}
	fmt.Println("violations:", CheckLeastPrivilege(rules))
}

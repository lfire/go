// 文件：api/contract.go
package main

import "fmt"

// Phase 是 CR 对外暴露的生命周期相位，取值集合固定，便于 kubectl 展示与告警分组。
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseUpgrading Phase = "Upgrading"
	PhaseDegraded  Phase = "Degraded"
)

// Cluster 是控制器依赖的唯一外部边界：所有读写都收在这张窄接口里。
// 生产用 client-go 实现，单测用 fake 实现，Reconcile 本身不碰任何 SDK。
type Cluster interface {
	Get(name string) (Spec, Observed, error)
	ApplyReplicas(name string, n int) error
	ApplyVersion(name string, v string) error
	Cleanup(name string) error
	RemoveFinalizer(name string) error
	UpdateStatus(name string, p Phase, resourceVersion string) error
}

// Spec 是用户声明的期望，Observed 是控制器读到的实际，两者都是值类型快照。
type Spec struct {
	Replicas int
	Version  string
}

type Observed struct {
	ReadyReplicas int
	Version       string
	Exists        bool
}

// noopCluster 用于演示接口可被最小实现满足，编译期即校验方法签名齐全。
type noopCluster struct{}

func (noopCluster) Get(string) (Spec, Observed, error) {
	return Spec{}, Observed{Exists: true}, nil
}
func (noopCluster) ApplyReplicas(string, int) error          { return nil }
func (noopCluster) ApplyVersion(string, string) error        { return nil }
func (noopCluster) Cleanup(string) error                     { return nil }
func (noopCluster) RemoveFinalizer(string) error             { return nil }
func (noopCluster) UpdateStatus(string, Phase, string) error { return nil }

func main() {
	var c Cluster = noopCluster{}
	_, got, err := c.Get("kv-prod")
	fmt.Println("observed exists:", got.Exists, "err:", err)
}

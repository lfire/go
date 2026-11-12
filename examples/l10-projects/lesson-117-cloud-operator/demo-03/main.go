// 文件：internal/reconcile/reconcile.go
package main

import (
	"errors"
	"fmt"
)

type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseUpgrading Phase = "Upgrading"
	PhaseDegraded  Phase = "Degraded"
)

type Action string

const (
	ActionNone    Action = "none"
	ActionScale   Action = "scale"
	ActionRollout Action = "rollout"
	ActionCleanup Action = "cleanup"
	ActionDrop    Action = "drop"
)

const FinalizerKey = "kv.example.com/cleanup"

// KVApp 是期望，Observed 是实际；Deleted 由 apiserver 的删除时间戳派生。
type KVApp struct {
	Replicas   int
	Version    string
	Deleted    bool
	Finalizers []string
}

type Observed struct {
	ReadyReplicas int
	Version       string
	Exists        bool
}

// Decide 是纯函数状态机：给定期望与实际，返回唯一动作与目标相位。
// 顺序即优先级：删除 > 版本 > 副本 > 收敛。一次只推进一个维度。
func Decide(want KVApp, got Observed) (Action, Phase) {
	if want.Deleted {
		if has(want.Finalizers, FinalizerKey) {
			return ActionCleanup, PhaseDegraded
		}
		return ActionDrop, PhaseDegraded
	}
	if got.Version != want.Version {
		return ActionRollout, PhaseUpgrading
	}
	switch {
	case got.ReadyReplicas < want.Replicas:
		return ActionScale, PhasePending
	case got.ReadyReplicas > want.Replicas:
		return ActionScale, PhasePending
	case got.ReadyReplicas == want.Replicas && got.ReadyReplicas > 0:
		return ActionNone, PhaseRunning
	default:
		return ActionNone, PhaseDegraded
	}
}

func has(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Cluster 是唯一外部边界，全部方法用绝对值语义，保证幂等。
type Cluster interface {
	Get(name string) (KVApp, Observed, error)
	ApplyReplicas(name string, n int) error
	ApplyVersion(name string, v string) error
	Cleanup(name string) error
	RemoveFinalizer(name string) error
	UpdateStatus(name string, p Phase, rv string) error
	ResourceVersion(name string) string
}

var ErrConflict = errors.New("status conflict")

// Reconcile 完成一次调谐，必须可重入：重复调用收敛到同一状态。
func Reconcile(c Cluster, name string) error {
	want, got, err := c.Get(name)
	if err != nil {
		return err
	}
	if !got.Exists {
		return nil // 对象已真正删除，无需动作
	}
	action, phase := Decide(want, got)
	switch action {
	case ActionScale:
		err = c.ApplyReplicas(name, want.Replicas)
	case ActionRollout:
		err = c.ApplyVersion(name, want.Version)
	case ActionCleanup:
		if err = c.Cleanup(name); err != nil {
			return err // 清理失败，保留 Finalizer，下一轮重试
		}
		return c.RemoveFinalizer(name) // 先清理成功，再移除，顺序不可颠倒
	case ActionDrop:
		return nil
	}
	if err != nil {
		return err
	}
	if action == ActionNone {
		return nil
	}
	if err = c.UpdateStatus(name, phase, c.ResourceVersion(name)); err != nil {
		if errors.Is(err, ErrConflict) {
			return err // 带退避重入，绝不覆盖他人写入
		}
		return err
	}
	return nil
}

// fakeCluster 是单测用的最小实现，记录动作而不碰任何网络。
type fakeCluster struct {
	want     KVApp
	got      Observed
	replicas int
	version  string
	cleaned  bool
	finLeft  bool
	status   Phase
}

func (f *fakeCluster) Get(string) (KVApp, Observed, error)            { return f.want, f.got, nil }
func (f *fakeCluster) ApplyReplicas(_ string, n int) error            { f.replicas = n; return nil }
func (f *fakeCluster) ApplyVersion(_ string, v string) error          { f.version = v; return nil }
func (f *fakeCluster) Cleanup(string) error                           { f.cleaned = true; return nil }
func (f *fakeCluster) RemoveFinalizer(string) error                   { f.finLeft = false; return nil }
func (f *fakeCluster) UpdateStatus(_ string, p Phase, _ string) error { f.status = p; return nil }
func (f *fakeCluster) ResourceVersion(string) string                  { return "rv-1" }

func main() {
	// 场景一：版本不一致优先升级，一次只做一个动作。
	c := &fakeCluster{
		want: KVApp{Replicas: 3, Version: "v2.0.0"},
		got:  Observed{ReadyReplicas: 1, Version: "v1.4.0", Exists: true},
	}
	_ = Reconcile(c, "kv")
	fmt.Println("rollout:", c.version, string(c.status), "replicas unchanged:", c.replicas == 0)

	// 场景二：删除走 Finalizer，先清理后移除。
	d := &fakeCluster{
		want:    KVApp{Deleted: true, Finalizers: []string{FinalizerKey}, Replicas: 3, Version: "v2.0.0"},
		got:     Observed{ReadyReplicas: 3, Version: "v2.0.0", Exists: true},
		finLeft: true,
	}
	_ = Reconcile(d, "kv")
	fmt.Println("cleanup:", d.cleaned, "finalizer left:", d.finLeft)

	// 场景三：连续调谐三次，验证幂等收敛。
	e := &fakeCluster{
		want: KVApp{Replicas: 3, Version: "v2.0.0"},
		got:  Observed{ReadyReplicas: 3, Version: "v2.0.0", Exists: true},
	}
	for i := 0; i < 3; i++ {
		_ = Reconcile(e, "kv")
	}
	fmt.Println("idempotent status:", string(e.status))
}

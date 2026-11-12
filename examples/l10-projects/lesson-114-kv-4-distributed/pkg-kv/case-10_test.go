// 文件：cluster_test.go
package kv

import (
	"errors"
	"fmt"
	"testing"
)

func TestQuorumMath(t *testing.T) {
	cases := []struct {
		n, w, r int
	}{
		{1, 1, 1},
		{3, 2, 2},
		{5, 3, 3},
		{7, 4, 4},
	}
	for _, c := range cases {
		if got := WriteQuorum(c.n); got != c.w {
			t.Fatalf("N=%d WriteQuorum=%d want=%d", c.n, got, c.w)
		}
		if got := ReadQuorum(c.n, c.w); got != c.r {
			t.Fatalf("N=%d ReadQuorum=%d want=%d", c.n, got, c.r)
		}
		if !QuorumValid(c.n, c.w, c.r) {
			t.Fatalf("N=%d W=%d R=%d 应满足 W+R>N", c.n, c.w, c.r)
		}
	}
}

func TestCheckQuorumSplitBrain(t *testing.T) {
	if err := CheckQuorum(3, 1); !errors.Is(err, ErrSplitBrain) {
		t.Fatalf("少数派分区应拒绝写, got %v", err)
	}
	if err := CheckQuorum(3, 2); err != nil {
		t.Fatalf("多数派分区应可写, got %v", err)
	}
	if err := CheckQuorum(5, 2); !errors.Is(err, ErrSplitBrain) {
		t.Fatalf("5 副本只剩 2 应拒绝写, got %v", err)
	}
}

func TestFrameCodeRoundTrip(t *testing.T) {
	errs := []error{
		ErrNotFound, ErrEmptyKey, ErrBadOp, ErrClosed,
		ErrCorrupt, ErrNoSpace, ErrPartialWrite,
		ErrNotLeader, ErrNoQuorum, ErrStaleRead,
		ErrSplitBrain, ErrMigrating, ErrCrossShardTxn,
	}
	for _, want := range errs {
		got := errOf(codeOf(want))
		if !errors.Is(got, want) {
			t.Fatalf("code=%d got=%v want=%v", codeOf(want), got, want)
		}
	}
}

func TestLeaderElection(t *testing.T) {
	core := newRaftCore(0, 3)
	if core.isLeader() {
		t.Fatal("初始状态应为 Follower")
	}
	term := core.startElection()
	if core.isLeader() {
		t.Fatal("3 副本自投一票不该当选")
	}
	if !core.recvVote() {
		t.Fatal("第二票应当选")
	}
	if !core.isLeader() {
		t.Fatal("应为 Leader")
	}
	core.stepDown(term + 1)
	if core.isLeader() {
		t.Fatal("发现更高任期应立即退位")
	}
	one := newRaftCore(0, 1)
	one.startElection()
	if !one.isLeader() {
		t.Fatal("单副本自投即达多数派")
	}
}

func TestQuorumFaultTolerance(t *testing.T) {
	g := NewReplicaGroup(0, 3)
	g.SetAckBudget(1) // 自己 1 票 + 1 个 Follower = 2 票，达到多数派
	if _, err := g.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	got, err := g.Apply(Command{Op: OpGet, Key: "k"})
	if err != nil || string(got.Value) != "v" {
		t.Fatalf("got=%q err=%v", got.Value, err)
	}

	g2 := NewReplicaGroup(1, 3)
	g2.SetAckBudget(0) // 只有自己 1 票，不足多数派
	if _, err := g2.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); !errors.Is(err, ErrNoQuorum) {
		t.Fatalf("want ErrNoQuorum, got %v", err)
	}
	if _, err := g2.Apply(Command{Op: OpGet, Key: "k"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未提交的写不应出现在状态机: %v", err)
	}
}

func TestConsistencyLevels(t *testing.T) {
	g := NewReplicaGroup(0, 3)
	if _, err := g.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	g.core.stepDown(g.core.currentTerm() + 1) // 模拟退位成 Follower
	if _, err := g.Read(Command{Op: OpGet, Key: "k"}, ConsistencyLinearizable); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("强一致读打到 Follower 应拒绝: %v", err)
	}
	if _, err := g.Read(Command{Op: OpGet, Key: "k"}, ConsistencyEventual); err != nil {
		t.Fatalf("最终一致读应允许 Follower: %v", err)
	}
}

func TestRouterWriteAcrossShards(t *testing.T) {
	m := NewMembership(64)
	for i := 0; i < 3; i++ {
		m.AddNode(fmt.Sprintf("node-%d", i), NewReplicaGroup(i, 3))
	}
	r := NewRouter(m)
	defer r.Close()
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("user:%d", i)
		if _, err := r.Apply(Command{Op: OpSet, Key: key, Value: []byte("paid")}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("user:%d", i)
		got, err := r.Apply(Command{Op: OpGet, Key: key})
		if err != nil || string(got.Value) != "paid" {
			t.Fatalf("key=%s err=%v val=%q", key, err, got.Value)
		}
	}
}

func TestCrossShardTxnRejected(t *testing.T) {
	m := NewMembership(64)
	for i := 0; i < 3; i++ {
		m.AddNode(fmt.Sprintf("node-%d", i), NewReplicaGroup(i, 3))
	}
	r := NewRouter(m)
	ring := m.Ring()
	base := ring.Get("anchor")
	other := ""
	for i := 0; i < 1000; i++ {
		k := fmt.Sprintf("key-%d", i)
		if ring.Get(k) != base {
			other = k
			break
		}
	}
	if other == "" {
		t.Fatal("构造失败：所有 key 落在同一分片")
	}
	if _, err := r.CheckSameShard([]string{"anchor", other}); !errors.Is(err, ErrCrossShardTxn) {
		t.Fatalf("want ErrCrossShardTxn, got %v", err)
	}
	if _, err := r.CheckSameShard([]string{"anchor", "anchor"}); err != nil {
		t.Fatalf("同分片应通过: %v", err)
	}
}

func TestHashRingMigration(t *testing.T) {
	ring := NewHashRing(128)
	ring.Add("a")
	const keys = 10000
	before := make([]string, keys)
	for i := 0; i < keys; i++ {
		before[i] = ring.Get(fmt.Sprintf("key-%d", i))
	}
	migs := ring.Add("b")
	if len(migs) == 0 {
		t.Fatal("扩容应产生迁移区间")
	}
	moved := 0
	for i := 0; i < keys; i++ {
		if ring.Get(fmt.Sprintf("key-%d", i)) != before[i] {
			moved++
		}
	}
	// 一致性哈希只迁移约 1/(N+1) 的 key；取模分片会接近全量迁移。
	if moved == 0 || moved > keys*9/10 {
		t.Fatalf("迁移比例异常 moved=%d", moved)
	}
	if ring.VNodeCount() != 256 {
		t.Fatalf("vnode=%d want=256", ring.VNodeCount())
	}
}

func TestMembershipLifecycle(t *testing.T) {
	m := NewMembership(64)
	m.AddNode("a", NewReplicaGroup(0, 3))
	if m.State() != MigrationIdle {
		t.Fatal("冷启动建集群不该进入 Joint")
	}
	m.AddNode("b", NewReplicaGroup(1, 3))
	if m.State() != MigrationJoint {
		t.Fatal("扩容应进入 Joint")
	}
	if len(m.Pending()) == 0 {
		t.Fatal("应有待迁移区间")
	}
	m.FinishMigration()
	if m.State() != MigrationDone {
		t.Fatal("校验一致后应推进到 Done")
	}
	m.Reset()
	if m.State() != MigrationIdle {
		t.Fatal("Reset 后应回到 Idle")
	}
}

func BenchmarkRouterSet(b *testing.B) {
	m := NewMembership(64)
	for i := 0; i < 3; i++ {
		m.AddNode(fmt.Sprintf("node-%d", i), NewReplicaGroup(i, 3))
	}
	r := NewRouter(m)
	defer r.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Apply(Command{Op: OpSet, Key: fmt.Sprintf("k%d", i%1024), Value: []byte("v")}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRouterGet(b *testing.B) {
	m := NewMembership(64)
	for i := 0; i < 3; i++ {
		m.AddNode(fmt.Sprintf("node-%d", i), NewReplicaGroup(i, 3))
	}
	r := NewRouter(m)
	defer r.Close()
	for i := 0; i < 1024; i++ {
		_ = mustApply(r, Command{Op: OpSet, Key: fmt.Sprintf("k%d", i), Value: []byte("v")})
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Apply(Command{Op: OpGet, Key: fmt.Sprintf("k%d", i%1024)}); err != nil {
			b.Fatal(err)
		}
	}
}

func mustApply(s Storage, cmd Command) Result {
	r, err := s.Apply(cmd)
	if err != nil {
		panic(err)
	}
	return r
}

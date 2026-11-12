// 文件：quorum.go
package kv

import "fmt"

// Consistency 是读一致性级别。越强越安全，也越依赖 Leader 与多数派。
type Consistency uint8

const (
	ConsistencyLinearizable     Consistency = iota // 读走 Leader 或 ReadIndex
	ConsistencyBoundedStaleness                    // 可读 Follower，落后有上限
	ConsistencyEventual                            // 任意副本，接受读到旧值
)

// WriteQuorum 返回写多数派票数：超过半数即可提交。
// 副本数取奇数正是为了让「多数」不产生歧义，3 副本容忍 1 个故障。
func WriteQuorum(replicas int) int {
	return replicas/2 + 1
}

// ReadQuorum 在 W + R > N 约束下返回强一致读所需的最小票数。
func ReadQuorum(replicas, write int) int {
	return replicas - write + 1
}

// QuorumValid 校验 W/R/N 配置能否保证读写交集，避免读到未提交的旧值。
func QuorumValid(replicas, write, read int) bool {
	return write > replicas/2 && write <= replicas && read >= 1 && write+read > replicas
}

// CheckQuorum 写前检查：可见副本不足多数派就拒绝提交。
// 网络分区后少数派若仍独立提交，就会与另一侧形成双主，即脑裂。
func CheckQuorum(replicas, reachable int) error {
	if reachable < WriteQuorum(replicas) {
		return fmt.Errorf("%w: reachable %d of %d", ErrSplitBrain, reachable, replicas)
	}
	return nil
}

// LevelAllowsFollowerRead 判断某一致性级别是否允许把读发到 Follower。
func LevelAllowsFollowerRead(level Consistency) bool {
	return level != ConsistencyLinearizable
}

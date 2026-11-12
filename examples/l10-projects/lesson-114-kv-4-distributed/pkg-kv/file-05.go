// 文件：raft.go
package kv

import "sync"

type RaftState uint8

const (
	StateFollower RaftState = iota
	StateCandidate
	StateLeader
)

type LogEntry struct {
	Term  uint64
	Index int
	Cmd   Command
}

// raftCore 是「任期 + 选主 + 日志复制 + 多数派提交」的最小可编译内核。
// 选举超时、心跳、日志持久化与网络传输交给 etcd/raft 这类成熟库；
// 这里用内存状态说明一致性来源：所有副本按同一顺序应用同一串命令。
type raftCore struct {
	mu           sync.Mutex
	me           int
	peers        int
	term         uint64
	votedFor     int
	state        RaftState
	votes        int
	log          []LogEntry
	commitIndex  int
	appliedIndex int
	matchIndex   []int
}

func newRaftCore(me, peers int) *raftCore {
	m := make([]int, peers)
	for i := range m {
		m[i] = -1
	}
	return &raftCore{
		me: me, peers: peers, votedFor: -1, state: StateFollower,
		commitIndex: -1, appliedIndex: -1, matchIndex: m,
	}
}

// startElection 发起一轮选举：任期加一、转为 Candidate、自投一票。
// 单副本时自投即达多数派；多副本需 recvVote 补齐票数。
func (r *raftCore) startElection() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.term++
	r.state = StateCandidate
	r.votedFor = r.me
	r.votes = 1
	if r.votes >= WriteQuorum(r.peers) {
		r.state = StateLeader
	}
	return r.term
}

// recvVote 收到一张来自其它节点的选票；达到多数派才转正为 Leader。
func (r *raftCore) recvVote() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.votes++
	if r.state == StateCandidate && r.votes >= WriteQuorum(r.peers) {
		r.state = StateLeader
		return true
	}
	return false
}

// stepDown 发现更高任期时立即退位，是防止双主（脑裂）的关键动作。
func (r *raftCore) stepDown(term uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if term > r.term {
		r.term = term
		r.state = StateFollower
		r.votedFor = -1
		r.votes = 0
	}
}

func (r *raftCore) isLeader() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state == StateLeader
}

func (r *raftCore) currentTerm() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.term
}

// logMatch 是 AppendEntries 的一致性检查：前一条日志的任期必须匹配，
// 不匹配则 Follower 拒绝，让 Leader 回退 nextIndex 后重发。
func (r *raftCore) logMatch(prevIndex int, prevTerm uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prevIndex < 0 {
		return true
	}
	if prevIndex >= len(r.log) {
		return false
	}
	return r.log[prevIndex].Term == prevTerm
}

// propose 只有 Leader 可调用：追加日志并给自己记一票，返回下标。
func (r *raftCore) propose(cmd Command) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != StateLeader {
		return -1, false
	}
	idx := len(r.log)
	r.log = append(r.log, LogEntry{Term: r.term, Index: idx, Cmd: cmd})
	r.matchIndex[r.me] = idx
	return idx, true
}

// ack 记录一个副本的复制确认，推进提交索引，返回本次新可应用的命令。
// 提交条件是「该下标被多数派确认」，而不是「所有副本都确认」，
// 因此慢副本不会拖住整条写链路。
func (r *raftCore) ack(peer, index int) []Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	if peer < 0 || peer >= r.peers || index < 0 || index >= len(r.log) {
		return nil
	}
	if index > r.matchIndex[peer] {
		r.matchIndex[peer] = index
	}
	for i := r.commitIndex + 1; i < len(r.log); i++ {
		n := 0
		for p := 0; p < r.peers; p++ {
			if r.matchIndex[p] >= i {
				n++
			}
		}
		if n < WriteQuorum(r.peers) {
			break
		}
		r.commitIndex = i
	}
	var out []Command
	for i := r.appliedIndex + 1; i <= r.commitIndex; i++ {
		out = append(out, r.log[i].Cmd)
	}
	r.appliedIndex = r.commitIndex
	return out
}

func (r *raftCore) committed(index int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return index >= 0 && index <= r.commitIndex
}

// lag 返回本地日志末尾与提交索引的差距，供有界陈旧读判断是否越界。
func (r *raftCore) lag() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.log) - 1 - r.commitIndex
}

func (r *raftCore) logLen() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.log)
}

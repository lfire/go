package main

import "fmt"

type Role string

const (
	Follower  Role = "Follower"
	Candidate Role = "Candidate"
	Leader    Role = "Leader"
)

// Node 是 Raft 节点的最小状态
type Node struct {
	id       string
	term     int
	role     Role
	votedFor string
}

// StartElection 发起选举：任期加一、先投给自己
func (n *Node) StartElection() {
	n.term++
	n.role = Candidate
	n.votedFor = n.id
}

// GrantVote 一任期只投一票，且只投给任期不小于自己的候选者
func (n *Node) GrantVote(candidate string, term int) bool {
	if term < n.term {
		return false
	}
	if term > n.term {
		n.term = term
		n.votedFor = ""
		n.role = Follower
	}
	if n.votedFor != "" && n.votedFor != candidate {
		return false
	}
	n.votedFor = candidate
	return true
}

// StepDown 收到更高任期消息时退位，这是脑裂后旧主收敛的关键
func (n *Node) StepDown(higherTerm int) {
	if higherTerm > n.term {
		n.term = higherTerm
		n.role = Follower
	}
}

func main() {
	n1 := &Node{id: "n1", role: Follower, term: 1}
	n2 := &Node{id: "n2", role: Follower, term: 1}
	n3 := &Node{id: "n3", role: Leader, term: 1} // 旧主落在少数派一侧

	n1.StartElection()
	votes := 1
	if n2.GrantVote("n1", n1.term) {
		votes++
	}
	if votes > 3/2 {
		n1.role = Leader
	}
	fmt.Println("新主 term:", n1.term, "得票:", votes, "，达到多数派:", votes > 3/2)
	fmt.Println("n2 同任期再投给旧主:", n2.GrantVote("n3", 2))
	fmt.Println("旧主自认为 Leader 但拿不到多数派:", n3.role == Leader)
	n3.StepDown(n1.term)
	fmt.Println("分区恢复后旧主退位为:", n3.role)
}

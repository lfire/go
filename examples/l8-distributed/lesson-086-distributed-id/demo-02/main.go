package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Snowflake 布局：41 位毫秒时间戳 + 10 位机器 ID + 12 位序列号
type Snowflake struct {
	mu     sync.Mutex
	epoch  int64
	nodeID int64
	lastMS int64
	seq    int64
	now    func() int64 // 可注入的时钟，便于测试时钟回拨
}

const (
	nodeBits = 10
	seqBits  = 12
	seqMask  = -1 ^ (-1 << seqBits) // 4095
	nodeMask = -1 ^ (-1 << nodeBits)
)

func NewSnowflake(epoch, nodeID int64) (*Snowflake, error) {
	if nodeID < 0 || nodeID > nodeMask {
		return nil, errors.New("机器 ID 超出范围")
	}
	return &Snowflake{
		epoch: epoch, nodeID: nodeID, lastMS: -1,
		now: func() int64 { return time.Now().UnixMilli() - epoch },
	}, nil
}

// NextID 生成下一个 ID；发现时钟回拨立即拒绝，避免发出重复号
func (s *Snowflake) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if now < s.lastMS {
		// 时钟回拨：继续沿用旧时间戳会与已经发出的 ID 撞号
		return 0, fmt.Errorf("时钟回拨 %d 毫秒，拒绝发号", s.lastMS-now)
	}
	if now == s.lastMS {
		s.seq = (s.seq + 1) & seqMask
		if s.seq == 0 {
			// 同一毫秒序列号用尽，自旋等到下一毫秒
			for now <= s.lastMS {
				now = s.now()
			}
		}
	} else {
		s.seq = 0
	}
	s.lastMS = now
	return (now << (nodeBits + seqBits)) | (s.nodeID << seqBits) | s.seq, nil
}

func main() {
	epoch := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	s, _ := NewSnowflake(epoch, 7)
	ids := make([]int64, 5)
	for i := range ids {
		ids[i], _ = s.NextID()
	}
	fmt.Println("连续 5 个 ID 趋势递增:", ids[0] < ids[1] && ids[1] < ids[2] && ids[2] < ids[3] && ids[3] < ids[4])

	// 注入一次时钟回拨：模拟 NTP 校时把系统时间往回调
	base := s.now()
	s.now = func() int64 { return base - 1000 }
	_, err := s.NextID()
	fmt.Println("时钟回拨后拒绝发号:", err != nil)
}

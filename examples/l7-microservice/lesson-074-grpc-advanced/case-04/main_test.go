package main

import (
	"context"
	"testing"
	"time"
)

func TestChildDeadlineNeverExceedsParent(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// 子 ctx 名义上给 300 毫秒，但父只剩 100 毫秒
	child, cancel2 := context.WithTimeout(parent, 300*time.Millisecond)
	defer cancel2()

	pd, _ := parent.Deadline()
	cd, _ := child.Deadline()
	if cd.After(pd) {
		t.Fatalf("子 deadline 不应晚于父 deadline")
	}
}

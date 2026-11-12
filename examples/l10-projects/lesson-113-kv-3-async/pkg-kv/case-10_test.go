// 文件：wal_test.go
package kv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordRoundTrip(t *testing.T) {
	cases := []Command{
		{Op: OpSet, Key: "user:1", Value: []byte("benji")},
		{Op: OpSet, Key: "k", Value: []byte("")},
		{Op: OpDel, Key: "gone"},
		{Op: OpSet, Key: "bin", Value: []byte{0, 1, 2, 255}},
	}
	for _, want := range cases {
		rec, err := encodeRecord(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeRecord(rec)
		if err != nil {
			t.Fatal(err)
		}
		if got.Op != want.Op || got.Key != want.Key || string(got.Value) != string(want.Value) {
			t.Fatalf("got %+v want %+v", got, want)
		}
	}
}

func TestDecodeRecordRejectsBitFlip(t *testing.T) {
	rec, err := encodeRecord(Command{Op: OpSet, Key: "k", Value: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	rec[len(rec)-1] ^= 0xFF
	if _, err := decodeRecord(rec); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

func TestFrameCodeRoundTrip(t *testing.T) {
	errs := []error{ErrNotFound, ErrEmptyKey, ErrBadOp, ErrClosed, ErrCorrupt, ErrNoSpace, ErrPartialWrite}
	for _, want := range errs {
		got := errOf(codeOf(want))
		if !errors.Is(got, want) {
			t.Fatalf("code=%d got=%v want=%v", codeOf(want), got, want)
		}
	}
}

func TestReplayStopsAtTornTail(t *testing.T) {
	dir := t.TempDir()
	f, err := os.OpenFile(filepath.Join(dir, "wal-000000.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		rec, _ := encodeRecord(Command{Op: OpSet, Key: fmt.Sprintf("k%d", i), Value: []byte("v")})
		if _, err := f.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	// 追加 5 字节，模拟崩溃时只写出半条记录头。
	if _, err := f.Write([]byte{0, 0, 0, 20, 0}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var applied []string
	off, err := Replay(dir, 0, &Metrics{}, func(cmd Command) error {
		applied = append(applied, cmd.Key)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 3 {
		t.Fatalf("want 3 applied, got %d (%v)", len(applied), applied)
	}
	if off <= 0 {
		t.Fatalf("want positive clean offset, got %d", off)
	}
}

func TestRecoverAcrossSnapshotAndRestart(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, "snapshot.gob")
	opts := WALOptions{Dir: dir, SyncPolicy: SyncEvery, SegmentSize: 1 << 20, Snapshot: snap}
	s, err := OpenWALStorage(opts)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err := s.Apply(Command{Op: OpSet, Key: fmt.Sprintf("k%03d", i), Value: []byte("v")}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Snapshot(snap); err != nil {
		t.Fatal(err)
	}
	for i := 100; i < 150; i++ {
		if _, err := s.Apply(Command{Op: OpSet, Key: fmt.Sprintf("k%03d", i), Value: []byte("v")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := OpenWALStorage(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	for i := 0; i < 150; i++ {
		got, err := s2.Apply(Command{Op: OpGet, Key: fmt.Sprintf("k%03d", i)})
		if err != nil || !got.Found {
			t.Fatalf("key k%03d: err=%v found=%v", i, err, got.Found)
		}
	}
	if s2.Metrics()["replay_records"] != 50 {
		t.Fatalf("want 50 replayed records, got %d", s2.Metrics()["replay_records"])
	}
}

func TestShardDistribution(t *testing.T) {
	counts := make([]int, shardCount)
	for i := 0; i < 10000; i++ {
		counts[shardOf(fmt.Sprintf("key-%d", i))]++
	}
	for i, c := range counts {
		if c == 0 {
			t.Fatalf("shard %d got 0 keys", i)
		}
	}
}

func BenchmarkWALSetGroup(b *testing.B) {
	opts := WALOptions{Dir: b.TempDir(), SyncPolicy: SyncGroup, GroupWindow: 5 * time.Millisecond}
	s, err := OpenWALStorage(opts)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWALSetEvery(b *testing.B) {
	opts := WALOptions{Dir: b.TempDir(), SyncPolicy: SyncEvery}
	s, err := OpenWALStorage(opts)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShardTableGet(b *testing.B) {
	table := NewShardTable()
	for i := 0; i < 1024; i++ {
		table.Set(fmt.Sprintf("k%d", i), []byte("v"))
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		table.Get(fmt.Sprintf("k%d", i%1024))
	}
}

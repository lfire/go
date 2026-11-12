package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeChunk(f *os.File, data []byte, index int64, chunkSize int64) error {
	_, err := f.WriteAt(data, index*chunkSize)
	return err
}

func TestWriteAtMakesChunkRetransmissionIdempotent(t *testing.T) {
	const chunkSize = 4
	path := filepath.Join(t.TempDir(), "upload.part")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	part0 := []byte("AAAA")
	part1 := []byte("BBBB")
	// 故意乱序到达，再重复重传 index 0
	_ = writeChunk(f, part1, 1, chunkSize)
	_ = writeChunk(f, part0, 0, chunkSize)
	_ = writeChunk(f, part0, 0, chunkSize)

	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, []byte("AAAABBBB")) {
		t.Fatalf("分片内容错乱: %q", got)
	}
}

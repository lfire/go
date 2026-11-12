package kv

import (
	"bytes"
	"errors"
	"net"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	want := Frame{Op: OpSet, Status: statusRequest, ReqID: 7, Key: "user:7", Value: []byte("benji"), Found: true}
	errCh := make(chan error, 1)
	go func() { errCh <- writeFrame(c1, want) }()
	got, err := readFrame(c2)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if got.Op != want.Op || got.ReqID != want.ReqID || got.Key != want.Key || string(got.Value) != "benji" || !got.Found {
		t.Fatalf("got %+v", got)
	}
}

func TestFrameRejectsPoison(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"bad magic", []byte{0x00, version, 1, 0, 0, 0, 0, 4, 0, 0, 0, 1, 0, 0, 0, 0}},
		{"bad version", []byte{magic, 9, 1, 0, 0, 0, 0, 4, 0, 0, 0, 1, 0, 0, 0, 0}},
		{"oversize len", []byte{magic, version, 1, 0, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0, 1}},
		{"short body", []byte{magic, version, 1, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0}},
		{"key len overflow", []byte{magic, version, 1, 0, 0, 0, 0, 4, 0, 0, 0, 1, 0xFF, 0xFF, 0, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := readFrame(bytes.NewReader(c.raw))
			if !errors.Is(err, ErrBadFrame) {
				t.Fatalf("want ErrBadFrame, got %v", err)
			}
		})
	}
}

func TestHalfFrameWithReadFull(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	done := make(chan error, 1)
	go func() {
		// 分三次写，模拟半包与粘包：接收方必须靠 ReadFull 读满整帧。
		full := &bytes.Buffer{}
		_ = writeFrame(full, Frame{Op: OpSet, Status: statusRequest, ReqID: 3, Key: "k", Value: []byte("v")})
		b := full.Bytes()
		_, _ = c1.Write(b[:5])
		_, _ = c1.Write(b[5:9])
		_, _ = c1.Write(b[9:])
		done <- nil
	}()
	got, err := readFrame(c2)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if got.Key != "k" || string(got.Value) != "v" || got.ReqID != 3 {
		t.Fatalf("got %+v", got)
	}
}

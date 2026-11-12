package main

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

const maxPixels = 40_000_000

func pngBomb() []byte {
	ihdr := []byte{0, 0, 0x75, 0x30, 0, 0, 0x75, 0x30, 8, 2, 0, 0, 0}
	body := append([]byte("IHDR"), ihdr...)
	crc := crc32.ChecksumIEEE(body)
	chunk := []byte{0, 0, 0, 13}
	chunk = append(chunk, body...)
	chunk = append(chunk, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
	sig := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	return append(sig, chunk...)
}

func checkPixels(head []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		return err
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return fmt.Errorf("像素超限: %dx%d", cfg.Width, cfg.Height)
	}
	return nil
}

func TestCheckPixels(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{"合法小图", buf.Bytes(), false},
		{"像素炸弹", pngBomb(), true},
		{"伪造后缀", []byte("<html>not image</html>"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkPixels(c.data)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func BenchmarkCheckPixels(b *testing.B) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = checkPixels(data)
	}
}

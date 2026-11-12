package main

import (
	"bytes"
	"fmt"
	"io"
	"time"
)

// ObjectMeta 是对象的权威描述，ETag 由键派生，不额外计算。
type ObjectMeta struct {
	Key         string
	ContentType string
	Size        int64
	ETag        string
	CreatedAt   time.Time
}

// ObjectStore 定义唯一可信副本，上传与下载只依赖这三个方法。
type ObjectStore interface {
	Put(key string, r io.Reader, ct string) (ObjectMeta, error)
	Get(key string) (io.ReadCloser, ObjectMeta, error)
	Delete(key string) error
}

// ImageProcessor 抽象派生能力，缩略图失败可降级不影响主链路。
type ImageProcessor interface {
	Thumbnail(r io.Reader, width int) (io.Reader, string, error)
}

type memStore struct {
	data map[string][]byte
	meta map[string]ObjectMeta
}

func newMemStore() *memStore {
	return &memStore{
		data: make(map[string][]byte),
		meta: make(map[string]ObjectMeta),
	}
}

func (m *memStore) Put(key string, r io.Reader, ct string) (ObjectMeta, error) {
	b, err := io.ReadAll(io.LimitReader(r, 8<<20))
	if err != nil {
		return ObjectMeta{}, err
	}
	m.data[key] = b
	meta := ObjectMeta{
		Key:         key,
		ContentType: ct,
		Size:        int64(len(b)),
		ETag:        fmt.Sprintf("%q", key),
		CreatedAt:   time.Now(),
	}
	m.meta[key] = meta
	return meta, nil
}

func (m *memStore) Get(key string) (io.ReadCloser, ObjectMeta, error) {
	b, ok := m.data[key]
	if !ok {
		return nil, ObjectMeta{}, fmt.Errorf("对象不存在: %s", key)
	}
	return io.NopCloser(bytes.NewReader(b)), m.meta[key], nil
}

func (m *memStore) Delete(key string) error {
	delete(m.data, key)
	delete(m.meta, key)
	return nil
}

func main() {
	var s ObjectStore = newMemStore()
	meta, err := s.Put("abc", bytes.NewReader([]byte("image-bytes")), "image/png")
	if err != nil {
		fmt.Println("写入失败:", err)
		return
	}
	rc, got, err := s.Get("abc")
	if err != nil {
		fmt.Println("读取失败:", err)
		return
	}
	defer rc.Close()
	fmt.Println("契约往返:", meta.Key, got.Size, got.ContentType, got.ETag)
}

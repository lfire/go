package main

import (
	"errors"
	"fmt"
	"strings"
)

// Storage 只声明命令层真正需要的三个方法，遵循小接口原则。
type Storage interface {
	Put(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
}

// Command 是一条可记录、可重放的操作，适合写 WAL 或做协议帧。
type Command struct {
	Op    string
	Key   string
	Value string
}

// Parse 把一行文本解析成 Command。
func Parse(line string) (Command, error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return Command{}, errors.New("空命令")
	}
	switch parts[0] {
	case "put":
		if len(parts) != 3 {
			return Command{}, errors.New("put 需要 key 与 value")
		}
		return Command{Op: "put", Key: parts[1], Value: parts[2]}, nil
	case "get", "del":
		if len(parts) != 2 {
			return Command{}, errors.New("该命令只接受一个 key")
		}
		return Command{Op: parts[0], Key: parts[1]}, nil
	default:
		return Command{}, fmt.Errorf("不支持的命令: %s", parts[0])
	}
}

// Apply 把 Command 应用到任意 Storage 实现。
func Apply(s Storage, c Command) (string, error) {
	switch c.Op {
	case "put":
		return "OK", s.Put(c.Key, c.Value)
	case "get":
		return s.Get(c.Key)
	case "del":
		return "OK", s.Delete(c.Key)
	default:
		return "", errors.New("未知操作")
	}
}

// tiny 是单线程演示用的最小实现；换成第 1 段的 MemStore 也兼容。
type tiny struct{ m map[string]string }

func (t *tiny) Put(k, v string) error { t.m[k] = v; return nil }
func (t *tiny) Get(k string) (string, error) {
	v, ok := t.m[k]
	if !ok {
		return "", errors.New("key 不存在")
	}
	return v, nil
}
func (t *tiny) Delete(k string) error { delete(t.m, k); return nil }

func main() {
	s := &tiny{m: make(map[string]string)}
	for _, line := range []string{"put lang go", "get lang", "del lang", "put ver 1.23"} {
		c, err := Parse(line)
		if err != nil {
			fmt.Println("解析失败:", err)
			continue
		}
		out, err := Apply(s, c)
		fmt.Println(line, "→", out, err == nil)
	}
}

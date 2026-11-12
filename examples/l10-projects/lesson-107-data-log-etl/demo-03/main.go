package main

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrBadLine 是坏行哨兵；所有解析失败都用 %w 包装，便于 errors.Is 分类统计。
var ErrBadLine = errors.New("日志行格式错误")

type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
	Raw    string
}

// parseLine 严格校验四个字段，任一失败都归入 ErrBadLine 家族。
func parseLine(line string) (Event, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 {
		return Event{}, fmt.Errorf("%w: 字段数=%d", ErrBadLine, len(fields))
	}
	ts, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || ts <= 0 {
		return Event{}, fmt.Errorf("%w: 时间戳=%q", ErrBadLine, fields[0])
	}
	status, err := strconv.Atoi(fields[2])
	if err != nil || status < 100 || status > 599 {
		return Event{}, fmt.Errorf("%w: 状态码=%q", ErrBadLine, fields[2])
	}
	size, err := strconv.Atoi(fields[3])
	if err != nil || size < 0 {
		return Event{}, fmt.Errorf("%w: 字节数=%q", ErrBadLine, fields[3])
	}
	return Event{TS: ts, Level: fields[1], Status: status, Bytes: size, Raw: line}, nil
}

// ReadEvents 逐行读取：坏行进 dead 保留原文，正常事件进 out。
// maxLine 显式放大 Scanner 单行上限；返回 sc.Err 才能发现超长行导致的停扫。
func ReadEvents(src string, maxLine int, out chan<- Event, dead chan<- string) error {
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		ev, err := parseLine(line)
		if err != nil {
			dead <- line
			continue
		}
		out <- ev
	}
	return sc.Err()
}

func main() {
	src := "1700000000 INFO 200 1024\nbad line\n1700000001 WARN 500 64\n"
	out := make(chan Event, 8)
	dead := make(chan string, 8)
	if err := ReadEvents(src, 1<<20, out, dead); err != nil {
		fmt.Println("扫描失败:", err)
	}
	close(out)
	close(dead)
	fmt.Printf("有效=%d 坏行=%d\n", len(out), len(dead))
}

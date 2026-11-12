package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// field 保存一个 cron 字段允许取值的集合。
type field struct {
	values map[int]bool
	min    int
	max    int
}

func parseField(expr string, min, max int) (field, error) {
	f := field{values: map[int]bool{}, min: min, max: max}
	for _, part := range strings.Split(expr, ",") {
		step := 1
		base := part
		if i := strings.Index(part, "/"); i >= 0 {
			base = part[:i]
			s, err := strconv.Atoi(part[i+1:])
			if err != nil || s <= 0 {
				return f, fmt.Errorf("非法步长 %q", part)
			}
			step = s
		}
		lo, hi := min, max
		if base != "*" {
			if j := strings.Index(base, "-"); j >= 0 {
				lo, _ = strconv.Atoi(base[:j])
				hi, _ = strconv.Atoi(base[j+1:])
			} else {
				lo, _ = strconv.Atoi(base)
				hi = lo
			}
		}
		if lo < min || hi > max || lo > hi {
			return f, fmt.Errorf("字段越界 %q", part)
		}
		for v := lo; v <= hi; v += step {
			f.values[v] = true
		}
	}
	return f, nil
}

// Cron 是 5 字段解析器：分 时 日 月 周。
type Cron struct {
	minute, hour, dom, month, dow field
}

func ParseCron(expr string) (Cron, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return Cron{}, fmt.Errorf("需要 5 个字段，实际 %d", len(parts))
	}
	var c Cron
	var err error
	if c.minute, err = parseField(parts[0], 0, 59); err != nil {
		return c, err
	}
	if c.hour, err = parseField(parts[1], 0, 23); err != nil {
		return c, err
	}
	if c.dom, err = parseField(parts[2], 1, 31); err != nil {
		return c, err
	}
	if c.month, err = parseField(parts[3], 1, 12); err != nil {
		return c, err
	}
	if c.dow, err = parseField(parts[4], 0, 6); err != nil {
		return c, err
	}
	return c, nil
}

func (f field) match(v int) bool { return f.values[v] }

// Next 从 after 之后逐分钟找下一个全部字段匹配的时刻，最多找两年。
func (c Cron) Next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(2 * 365 * 24 * time.Hour)
	for ; t.Before(limit); t = t.Add(time.Minute) {
		if c.month.match(int(t.Month())) &&
			c.dom.match(t.Day()) &&
			c.dow.match(int(t.Weekday())) &&
			c.hour.match(t.Hour()) &&
			c.minute.match(t.Minute()) {
			return t
		}
	}
	return time.Time{}
}

func main() {
	c, err := ParseCron("*/15 9-18 * * 1-5")
	if err != nil {
		fmt.Println("解析失败:", err)
		return
	}
	now := time.Date(2026, 1, 5, 9, 2, 0, 0, time.UTC) // 周一 9:02
	fmt.Println("下次触发:", c.Next(now).Format("2006-01-02 15:04"))
}

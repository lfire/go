package kv

import (
	"errors"
	"fmt"
	"strings"
)

// ParseCommand 把一行文本解析成 Command，供 CLI 与第 112 讲网络层复用。
// 形如 GET k / SET k v / DEL k；value 含空格时用 Join 还原。
func ParseCommand(line string) (Command, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Command{}, fmt.Errorf("%w: empty line", ErrBadOp)
	}
	var cmd Command
	switch strings.ToUpper(fields[0]) {
	case "GET":
		cmd.Op = OpGet
	case "SET":
		cmd.Op = OpSet
	case "DEL":
		cmd.Op = OpDel
	default:
		return Command{}, fmt.Errorf("%w: %s", ErrBadOp, fields[0])
	}
	if len(fields) < 2 {
		return Command{}, fmt.Errorf("%w: missing key", ErrEmptyKey)
	}
	cmd.Key = fields[1]
	if cmd.Op == OpSet {
		if len(fields) < 3 {
			return Command{}, fmt.Errorf("%w: missing value", ErrEmptyKey)
		}
		cmd.Value = []byte(strings.Join(fields[2:], " "))
	}
	return cmd, nil
}

// FormatResult 把 Result 与 error 收敛成一行文本，便于 CLI 打印与网络回包。
// 错误统一映射成稳定的 ERR 码，内部仍靠哨兵错误判定，不比对字符串。
func FormatResult(r Result, err error) string {
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			return "ERR not_found"
		case errors.Is(err, ErrClosed):
			return "ERR closed"
		case errors.Is(err, ErrEmptyKey):
			return "ERR empty_key"
		case errors.Is(err, ErrBadOp):
			return "ERR bad_op"
		default:
			return "ERR " + err.Error()
		}
	}
	if !r.Found {
		return "NIL"
	}
	return "OK " + string(r.Value)
}

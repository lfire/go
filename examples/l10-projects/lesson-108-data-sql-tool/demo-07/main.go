package main

import (
	"bufio"
	"fmt"
	"strings"
)

// dispatch 把一行命令分派到 EXPLAIN 或 SELECT，是 REPL 的最小内核。
func dispatch(line string) string {
	line = strings.TrimSpace(line)
	switch {
	case line == "":
		return ""
	case strings.HasPrefix(strings.ToUpper(line), "EXPLAIN"):
		return "Project status,size\n  Filter(pushed down) status = 200\n  TableScan streaming=true"
	case strings.HasPrefix(strings.ToUpper(line), "SELECT"):
		return "status,size\n200,2048\n200,128"
	case line == ".exit" || line == "quit":
		return "BYE"
	default:
		return "ERR: 仅支持 SELECT、EXPLAIN 与 .exit"
	}
}

// RunREPL 逐行读命令并输出；返回处理的命令数。
func RunREPL(in *bufio.Scanner, out func(string)) int {
	n := 0
	for in.Scan() {
		line := in.Text()
		res := dispatch(line)
		if res == "BYE" {
			return n
		}
		if res == "" {
			continue
		}
		out(res)
		n++
	}
	if err := in.Err(); err != nil {
		out(fmt.Sprintf("ERR: 读取失败 %v", err))
	}
	return n
}

func main() {
	cmds := "EXPLAIN SELECT status FROM access WHERE status = 200\nSELECT status,size FROM access\n.exit\n"
	sc := bufio.NewScanner(strings.NewReader(cmds))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	n := RunREPL(sc, func(s string) { fmt.Println(s) })
	fmt.Println("执行命令数:", n)
}

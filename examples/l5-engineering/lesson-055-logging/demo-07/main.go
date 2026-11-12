package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

const G, LINES, PAYLOAD = 8, 200, 40

func scenario(path string, useAppend bool) (int, int) {
	os.Remove(path)
	flags := os.O_WRONLY | os.O_CREATE
	if useAppend {
		flags |= os.O_APPEND
	}
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			f, err := os.OpenFile(path, flags, 0644)
			if err != nil {
				return
			}
			defer f.Close()
			for i := 0; i < LINES; i++ {
				line := fmt.Sprintf("g=%d i=%03d p=%s\n", g, i, strings.Repeat("x", PAYLOAD))
				_, _ = f.WriteString(line)
			}
		}(g)
	}
	wg.Wait()
	f, _ := os.Open(path)
	defer f.Close()
	total, complete := 0, 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		total++
		l := sc.Text()
		if strings.HasPrefix(l, "g=") && strings.HasSuffix(l, strings.Repeat("x", PAYLOAD)) {
			complete++
		}
	}
	return total, complete
}

func main() {
	t, c := scenario("/tmp/append_on.log", true)
	fmt.Printf("O_APPEND   : 行数=%d 完整=%d (预期=%d)\n", t, c, G*LINES)
	t2, c2 := scenario("/tmp/append_off.log", false)
	fmt.Printf("无 O_APPEND: 行数=%d 完整=%d (预期=%d)\n", t2, c2, G*LINES)
}

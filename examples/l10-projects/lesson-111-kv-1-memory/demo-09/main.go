// 文件：cmd/kvdemo/main.go
package main

import (
	"fmt"
	"log"

	kvstore "github.com/lfire/go/examples/l10-projects/lesson-111-kv-1-memory/pkg-kv" // 包名为 kv，显式别名让调用点读起来与模块名一致
)

func main() {
	store := kvstore.NewMeteredStorage(kvstore.NewMemory())
	defer store.Close()
	lines := []string{"SET lang go", "SET ver 1.23", "GET lang", "DEL ver", "GET ver"}
	for _, line := range lines {
		cmd, err := kvstore.ParseCommand(line)
		if err != nil {
			log.Printf("parse %q: %v", line, err)
			continue
		}
		res, err := store.Apply(cmd)
		fmt.Printf("%-14s → %s\n", line, kvstore.FormatResult(res, err))
	}
	calls, errs, avgUs := store.Snapshot()
	fmt.Printf("calls=%d errs=%d avg=%.2fus\n", calls, errs, avgUs)
}

// 文件：cmd/kvd/main.go
package main

import (
	"fmt"
	"log"

	kvstore "github.com/lfire/go/examples/l10-projects/lesson-112-kv-2-network/pkg-kv" // 包名为 kv，显式别名让调用点读起来与模块名一致
)

func main() {
	srv, err := kvstore.NewServer("127.0.0.1:0", kvstore.NewMemory())
	if err != nil {
		log.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	defer srv.Close()

	pool, err := kvstore.NewPool(srv.Addr(), 4)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	for _, line := range []string{"SET lang go", "GET lang", "GET missing"} {
		cmd, err := kvstore.ParseCommand(line)
		if err != nil {
			log.Printf("parse: %v", err)
			continue
		}
		res, err := pool.Apply(cmd)
		fmt.Printf("%-14s → %s\n", line, kvstore.FormatResult(res, err))
	}
}

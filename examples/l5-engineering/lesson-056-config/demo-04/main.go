package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

type Cfg struct{ A, B int }

var bad Cfg
var good atomic.Value

func main() {
	good.Store(Cfg{A: 0, B: 0})
	const N = 200000
	var tornBad, tornGood int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for n := 1; n <= N; n++ {
			bad.A = n
			runtime.Gosched()
			bad.B = n
			good.Store(Cfg{A: n, B: n})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < N; i++ {
			c := bad
			if c.A != c.B {
				tornBad++
			}
			g := good.Load().(Cfg)
			if g.A != g.B {
				tornGood++
			}
		}
	}()
	wg.Wait()
	fmt.Println("逐字段写出现撕裂快照:", tornBad > 0, "次数:", tornBad)
	fmt.Println("atomic.Value 出现撕裂快照:", tornGood > 0)
	g := good.Load().(Cfg)
	fmt.Println("atomic.Value 最终快照:", g)

	func() {
		defer func() { fmt.Println("换类型 Store:", recover()) }()
		good.Store("不是 Cfg")
	}()
}

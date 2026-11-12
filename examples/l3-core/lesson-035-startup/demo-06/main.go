// heavy.go
//go:build heavy

package main

import (
	"fmt"
	_ "net/http"
	"syscall"
)

func init() {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	fmt.Printf("heavy  Utime=%d.%06ds Stime=%d.%06ds Minflt=%d Maxrss=%d\n",
		ru.Utime.Sec, ru.Utime.Usec, ru.Stime.Sec, ru.Stime.Usec, ru.Minflt, ru.Maxrss)
}

func main() {}

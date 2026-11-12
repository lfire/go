package main

import (
	"fmt"
	"runtime/debug"
)

var Version = "dev"
var Empty string
var Count int

const Const = "fixed"

func main() {
	fmt.Println("Version =", Version, " Empty =", Empty, " Count =", Count, " Const =", Const)
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "-ldflags" || s.Key == "vcs.revision" || s.Key == "vcs.modified" {
				fmt.Println("buildinfo:", s.Key, "=", s.Value)
			}
		}
	}
}

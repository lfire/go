package main

import (
	"golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(printf.Analyzer) }

package main

import "fmt"

type Repo interface{ Get(id int) string }

type mem struct{}

func (m mem) Get(id int) string { return "ada" }

func call(r Repo) string { return r.Get(1) }

func plain() string { return "fixed" }

func main() {
	fmt.Println(call(mem{}))
	fmt.Println(plain())
}

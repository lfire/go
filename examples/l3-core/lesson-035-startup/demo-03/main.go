// init3.go：3 个 init
package main

import "fmt"

var sink int

func init() { sink++ }
func init() { sink++ }
func init() { sink++ }

func main() { fmt.Println(sink) }

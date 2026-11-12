// init1.go：只留 1 个 init，其余完全相同
package main

import "fmt"

var sink int

func init() { sink++ }

func main() { fmt.Println(sink) }

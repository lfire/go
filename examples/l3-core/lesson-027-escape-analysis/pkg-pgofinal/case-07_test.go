// lib_test.go
package pgofinal

import "testing"

var Sink int

func BenchmarkHot(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Sink = Hot(100)
	}
}

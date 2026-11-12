// wb_test.go
package purewb

import "testing"

func BenchmarkPtrStores(b *testing.B) {
	for i := 0; i < b.N; i++ {
		PtrStores(1024)
	}
}

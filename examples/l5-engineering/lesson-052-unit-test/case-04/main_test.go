package p

import (
	"testing"
	"time"
)

func TestParallel(t *testing.T) {
	for _, name := range []string{"a", "b", "c", "d"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			time.Sleep(30 * time.Millisecond)
		})
	}
}

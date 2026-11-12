package main

import (
	"fmt"
	"testing"
)

func inconsistentWindow() (db, cache string, consistent bool) {
	var dbv, cachev = "old", "old"
	deleted := make(chan struct{})
	updated := make(chan struct{})
	done := make(chan struct{})

	go func() { // 写线程：先删缓存，等读线程回填后再更新库
		cachev = ""
		close(deleted)
		<-updated
		dbv = "new"
		close(done)
	}()

	<-deleted
	if cachev == "" {
		cachev = dbv // 回源，此刻库里还是 old
	}
	close(updated)
	<-done
	return dbv, cachev, dbv == cachev
}

func TestDelayedDoubleDeleteWindow(t *testing.T) {
	db, cache, consistent := inconsistentWindow()
	fmt.Println("db =", db, "cache =", cache, "一致 =", consistent)
	if consistent {
		t.Fatal("该交错下应当出现不一致，实验才说明问题")
	}
}

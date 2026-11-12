package main

import "testing"

type fakeDB struct{ queries int }

func (d *fakeDB) listUsers() []int {
	d.queries++
	return []int{1, 2, 3, 4, 5}
}

func (d *fakeDB) listOrders(userID int) {
	d.queries++
}

func (d *fakeDB) listOrdersIn(ids []int) {
	d.queries++
}

func slow(d *fakeDB) {
	for _, id := range d.listUsers() {
		d.listOrders(id)
	}
}

func fast(d *fakeDB) {
	ids := d.listUsers()
	d.listOrdersIn(ids)
}

func TestNPlusOneVsPreload(t *testing.T) {
	slowDB, fastDB := &fakeDB{}, &fakeDB{}
	slow(slowDB)
	fast(fastDB)
	if slowDB.queries != 6 {
		t.Fatalf("循环写法 SQL=%d，应为 1+5=6", slowDB.queries)
	}
	if fastDB.queries != 2 {
		t.Fatalf("预加载 SQL=%d，应为固定 2", fastDB.queries)
	}
}

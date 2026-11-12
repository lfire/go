package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type User struct {
	ID     uint    `gorm:"primaryKey"`
	Name   string  `gorm:"size:64;not null"`
	Orders []Order `gorm:"foreignKey:UserID"`
}

type Order struct {
	ID     uint `gorm:"primaryKey"`
	UserID uint `gorm:"index"`
	Amount int
}

func main() {
	// LogMode(logger.Warn) 只打慢查询与错误；开发期可临时开 logger.Info 看全量 SQL。
	db, err := gorm.Open(sqlite.Open("file:app.db?_pragma=busy_timeout(5000)"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		log.Fatal(err)
	}
	// AutoMigrate 只适合开发与测试；生产用显式版本化迁移（见第 3 段）。
	if err := db.AutoMigrate(&User{}, &Order{}); err != nil {
		log.Fatal(err)
	}
	seed(db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/slow", slowList(db))
	mux.HandleFunc("GET /users/fast", fastList(db))
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func seed(db *gorm.DB) {
	var n int64
	db.Model(&User{}).Count(&n)
	if n > 0 {
		return // 避免每次启动都重复插入
	}
	for i := 1; i <= 3; i++ {
		u := User{Name: fmt.Sprintf("user-%d", i),
			Orders: []Order{{Amount: i * 10}}}
		db.Create(&u)
	}
}

// slowList 演示 N+1：查完用户后，在循环里逐个查订单。
func slowList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var users []User
		db.WithContext(r.Context()).Find(&users)
		for i := range users {
			db.WithContext(r.Context()).
				Model(&users[i]).Association("Orders").Find(&users[i].Orders)
		}
		writeJSON(w, users)
	}
}

// fastList 用 Preload 一次 IN 查询取回全部关联，SQL 条数固定为 2。
func fastList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var users []User
		db.WithContext(r.Context()).Preload("Orders").Find(&users)
		writeJSON(w, users)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

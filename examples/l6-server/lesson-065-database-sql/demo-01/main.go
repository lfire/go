package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite" // 只导入驱动，靠 init 注册；代码里只用 database/sql
)

type User struct {
	ID   int
	Name string
	Age  int
}

func main() {
	// 用 run() 收敛错误，保证所有 defer 都能执行；直接 log.Fatal 会跳过 defer。
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Open 只构造连接池，不建连；驱动名要与 import 注册的名字一致。
	db, err := sql.Open("sqlite", "file:app.db?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()

	// 四个参数决定池的行为，见第 3 节表格。
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil { // 此刻才真正建连
		return err
	}

	const ddl = `CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		age INTEGER
	)`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return err
	}

	// Exec 用于不返回行的语句；用 ? 号绑定参数，绝不拼接用户输入。
	res, err := db.ExecContext(ctx,
		"INSERT INTO users(name, age) VALUES(?, ?)", "gopher", 7)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fmt.Println("inserted id:", id)

	// QueryRow 用于至多一行；Scan 的参数个数与列顺序必须严格对应。
	var u User
	err = db.QueryRowContext(ctx,
		"SELECT id, name, age FROM users WHERE id = ?", id).
		Scan(&u.ID, &u.Name, &u.Age)
	if err != nil {
		return err
	}
	fmt.Printf("query row: %+v\n", u)

	// Query 返回多行：务必检查 rows.Err，并显式关闭。
	rows, err := db.QueryContext(ctx,
		"SELECT id, name, age FROM users WHERE age >= ?", 0)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var x User
		if err := rows.Scan(&x.ID, &x.Name, &x.Age); err != nil {
			return err
		}
		fmt.Printf("row: %+v\n", x)
	}
	return rows.Err() // 遍历中途的错误只在这里暴露
}

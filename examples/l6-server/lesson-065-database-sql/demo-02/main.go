package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

var errInsufficient = errors.New("余额不足")

func main() {
	if err := transfer(); err != nil {
		log.Fatal(err)
	}
}

func transfer() error {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // SQLite 单写，演示用；生产 MySQL 用较大值

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const ddl = `CREATE TABLE accounts (id INTEGER PRIMARY KEY, balance INTEGER NOT NULL)`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO accounts(id, balance) VALUES(1, 100), (2, 0)`); err != nil {
		return err
	}

	// BeginTx 从池里独占一条连接直到事务结束，期间所有操作都必须走 tx。
	// 生产 MySQL/Postgres 可指定隔离级别；SQLite 驱动只支持 LevelDefault。
	tx, err := db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelDefault,
		ReadOnly:  false,
	})
	if err != nil {
		return err
	}
	// Commit 之后再 Rollback 会返回 ErrTxDone，可安全忽略。
	defer tx.Rollback()

	var balance int
	if err := tx.QueryRowContext(ctx,
		`SELECT balance FROM accounts WHERE id = ?`, 1).Scan(&balance); err != nil {
		return err
	}
	if balance < 30 {
		return errInsufficient
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance = balance - 30 WHERE id = ?`, 1); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE accounts SET balance = balance + 30 WHERE id = ?`, 2); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	fmt.Println("转账成功")
	return nil
}

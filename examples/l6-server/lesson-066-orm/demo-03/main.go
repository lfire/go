package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

type migration struct {
	version int
	name    string
	up      string
}

var migrations = []migration{
	{1, "create_users", `CREATE TABLE users (
		id INTEGER PRIMARY KEY, name TEXT NOT NULL)`},
	{2, "add_email", `ALTER TABLE users ADD COLUMN email TEXT`},
}

func migrate(db *sql.DB) error {
	const versionTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`
	if _, err := db.Exec(versionTable); err != nil {
		return err
	}
	for _, m := range migrations {
		var exists int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
			m.version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue // 已执行过，保证可重复运行
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.up); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate %d: %w", m.version, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations(version, name, applied_at)
			 VALUES(?, ?, datetime('now'))`, m.version, m.name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		fmt.Println("applied migration", m.version, m.name)
	}
	return nil
}

func main() {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		log.Fatal(err)
	}
	if err := migrate(db); err != nil { // 第二次应当一条都不执行
		log.Fatal(err)
	}
	var cnt int
	_ = db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&cnt)
	fmt.Println("已记录迁移数:", cnt)
}

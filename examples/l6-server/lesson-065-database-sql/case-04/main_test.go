package main

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNullScanAndQueryContract(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"name", "email"}).
		AddRow("gopher", nil) // 模拟可空列为 NULL
	mock.ExpectQuery("SELECT name, email FROM users").WillReturnRows(rows)

	var name, email string
	if err := db.QueryRow("SELECT name, email FROM users").Scan(&name, &email); err == nil {
		t.Fatal("把 NULL 扫进 string 竟然成功了，三值语义被吞掉")
	}

	rows2 := sqlmock.NewRows([]string{"name", "email"}).AddRow("gopher", nil)
	mock.ExpectQuery("SELECT name, email FROM users").WillReturnRows(rows2)
	var ns sql.NullString
	if err := db.QueryRow("SELECT name, email FROM users").Scan(&name, &ns); err != nil {
		t.Fatalf("sql.NullString 应能接受 NULL: %v", err)
	}
	if ns.Valid || ns.String != "" {
		t.Fatalf("NULL 应令 Valid=false: %+v", ns)
	}

	mock.ExpectExec(`INSERT INTO users\(name, email\) VALUES\(\?, \?\)`).
		WithArgs("gopher", "g@x.com").
		WillReturnResult(sqlmock.NewResult(1, 1))
	if _, err := db.Exec("INSERT INTO users(name, email) VALUES(?, ?)", "gopher", "g@x.com"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

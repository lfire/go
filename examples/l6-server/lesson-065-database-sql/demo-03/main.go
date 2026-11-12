package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	_ "modernc.org/sqlite"
)

// db 是进程级单例：sql.DB 本身就是连接池，绝不在 Handler 里反复 Open。
var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("sqlite", "file:demo03.db?_pragma=busy_timeout(5000)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := initSchema(); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", getUser)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func initSchema() error {
	const ddl = `CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT, age INTEGER
	)`
	if _, err := db.Exec(ddl); err != nil {
		return err
	}
	_, err := db.Exec(
		`INSERT OR IGNORE INTO users(id, name, email, age) VALUES(1, 'gopher', NULL, NULL)`)
	return err
}

type userDTO struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

func getUser(w http.ResponseWriter, r *http.Request) {
	var name string
	var email sql.NullString
	var age sql.NullInt64
	err := db.QueryRowContext(r.Context(),
		`SELECT name, email, age FROM users WHERE id = ?`, r.PathValue("id")).
		Scan(&name, &email, &age)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	dto := userDTO{ID: 1, Name: name}
	if email.Valid {
		dto.Email = email.String
	}
	if age.Valid {
		dto.Age = int(age.Int64)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(dto)
}

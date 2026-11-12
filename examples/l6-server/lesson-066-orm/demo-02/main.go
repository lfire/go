package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type User struct {
	ID   int    `db:"id" json:"id"`
	Name string `db:"name" json:"name"`
	Age  int    `db:"age" json:"age"`
}

var db *sqlx.DB

func main() {
	var err error
	// sqlx.Open 只是 database/sql 的薄封装，连接池仍由底层 sql.DB 管理。
	db, err = sqlx.Open("sqlite", "file:app.db?_pragma=busy_timeout(5000)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.MustExec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY, name TEXT, age INTEGER)`)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /users", listUsers)
	mux.HandleFunc("POST /users", createUser)
	mux.HandleFunc("GET /users/{id}", getUser)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func listUsers(w http.ResponseWriter, r *http.Request) {
	var users []User
	// SelectContext 把多行直接扫进切片，省掉手写 rows.Next 与 Scan。
	if err := db.SelectContext(r.Context(), &users,
		`SELECT id, name, age FROM users ORDER BY id`); err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func getUser(w http.ResponseWriter, r *http.Request) {
	var u User
	err := db.GetContext(r.Context(), &u,
		`SELECT id, name, age FROM users WHERE id = ?`, r.PathValue("id"))
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var req User
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	// NamedExec 支持 :name 形式的命名参数，仍由驱动做参数绑定。
	res, err := db.NamedExecContext(r.Context(),
		`INSERT INTO users(name, age) VALUES(:name, :age)`, req)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	req.ID = int(id)
	writeJSON(w, http.StatusCreated, req)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

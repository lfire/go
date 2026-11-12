package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"sync"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// store 是线程安全的内存仓储，仅用于演示；生产应换成数据库。
type store struct {
	mu     sync.Mutex
	nextID int
	users  map[int]User
}

func (s *store) list(offset, limit int, role string) []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int, 0, len(s.users))
	for id, u := range s.users {
		if role == "" || u.Role == role {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids) // 保证分页稳定；生产里应由 ORDER BY 完成
	if offset > len(ids) {
		offset = len(ids)
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	out := make([]User, 0, end-offset)
	for _, id := range ids[offset:end] {
		out = append(out, s.users[id])
	}
	return out
}

func (s *store) get(id int) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	return u, ok
}

func (s *store) create(u User) User {
	s.mu.Lock()
	defer s.mu.Unlock()
	u.ID = s.nextID
	s.nextID++
	s.users[u.ID] = u
	return u
}

// put 是幂等的：无论调用多少次，最终状态完全由本次请求体决定。
func (s *store) put(id int, u User) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return User{}, false
	}
	u.ID = id
	s.users[id] = u
	return u, true
}

func (s *store) remove(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return false
	}
	delete(s.users, id)
	return true
}

func parseID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil && id > 0
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *store) handleList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	users := s.list(offset, limit, r.URL.Query().Get("role"))
	writeJSON(w, http.StatusOK, map[string]any{
		"data": users, "offset": offset, "limit": limit,
	})
}

func (s *store) handleCreate(w http.ResponseWriter, r *http.Request) {
	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	created := s.create(u)
	// 201 必须带 Location，告诉客户端新资源在哪。
	w.Header().Set("Location", "/v1/users/"+strconv.Itoa(created.ID))
	writeJSON(w, http.StatusCreated, created)
}

func (s *store) handleGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	u, ok := s.get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *store) handlePut(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	updated, ok := s.put(id, u)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *store) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if !s.remove(id) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent) // 204 不带响应体
}

func main() {
	s := &store{nextID: 1, users: make(map[int]User)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/users", s.handleList)
	mux.HandleFunc("POST /v1/users", s.handleCreate)
	mux.HandleFunc("GET /v1/users/{id}", s.handleGet)
	mux.HandleFunc("PUT /v1/users/{id}", s.handlePut)
	mux.HandleFunc("DELETE /v1/users/{id}", s.handleDelete)

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

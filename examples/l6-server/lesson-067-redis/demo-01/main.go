package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

var rdb *redis.Client

// 模拟源数据库；生产里换成 db.QueryRowContext。
var db = map[string]string{"1": "gopher", "2": "alice"}

const (
	baseTTL   = 5 * time.Minute
	emptyTTL  = 60 * time.Second // 空值缓存只挡一小段时间
	emptyMark = "__NULL__"        // 约定一个不会与业务值冲突的哨兵
)

func queryDB(id string) (string, bool) {
	v, ok := db[id]
	return v, ok
}

// ttlWithJitter 在基础 TTL 上加 0~20% 随机量，让 key 分散过期。
func ttlWithJitter(base time.Duration) time.Duration {
	jitter := time.Duration(rand.Int63n(int64(base / 5)))
	return base + jitter
}

func getUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	key := "user:" + id
	c := r.Context()

	val, err := rdb.Get(c, key).Result()
	if err == nil { // 命中
		if val == emptyMark {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK,
			map[string]string{"id": id, "name": val, "from": "cache"})
		return
	}
	// 区分「没命中」与「缓存故障」：后者必须降级回源，不能让整个接口挂掉。
	if !errors.Is(err, redis.Nil) {
		log.Printf("redis get: %v", err)
	}

	name, ok := queryDB(id)
	if !ok {
		// 查不到也缓存一小段空值，挡住对不存在 id 的反复穿透。
		_ = rdb.Set(c, key, emptyMark, emptyTTL).Err()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_ = rdb.Set(c, key, name, ttlWithJitter(baseTTL)).Err()
	writeJSON(w, http.StatusOK,
		map[string]string{"id": id, "name": name, "from": "db"})
}

func main() {
	rdb = redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 0})
	defer rdb.Close()
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", getUser)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

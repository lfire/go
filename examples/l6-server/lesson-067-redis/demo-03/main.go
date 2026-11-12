package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// tryLock 用 SETNX 加唯一 token 与 TTL，实现最小可用的互斥与租约。
func tryLock(ctx context.Context, rdb *redis.Client, key, token string, ttl time.Duration) (bool, error) {
	return rdb.SetNX(ctx, key, token, ttl).Result()
}

// 释放锁必须「先比对 token 再删除」，两步用 Lua 保证原子，
// 否则可能把别人续期后持有的锁误删。
var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`)

func releaseLock(ctx context.Context, rdb *redis.Client, key, token string) error {
	return releaseScript.Run(ctx, rdb, []string{key}, token).Err()
}

// updateUser 演示延迟双删：先删、更新库、延迟再删一次，
// 用来清掉「读请求夹在删除与更新之间把旧值回填」的脏数据。
func updateUser(ctx context.Context, rdb *redis.Client, id, name string) error {
	key := "user:" + id
	if err := rdb.Del(ctx, key).Err(); err != nil {
		return err
	}
	fmt.Println("数据库已更新:", id, name) // 生产里是 writeDB
	// 用独立 context：原请求的 ctx 在返回后可能已取消。
	time.AfterFunc(500*time.Millisecond, func() {
		_ = rdb.Del(context.Background(), key).Err()
	})
	return nil
}

func main() {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer rdb.Close()
	ctx := context.Background()
	key := "lock:rebuild:user:1"
	token := fmt.Sprintf("%d", time.Now().UnixNano())

	ok, err := tryLock(ctx, rdb, key, token, 10*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		fmt.Println("已有实例在重建，本实例跳过")
		return
	}
	defer func() { _ = releaseLock(ctx, rdb, key, token) }()

	fmt.Println("拿到锁，执行缓存重建")
	if err := updateUser(ctx, rdb, "1", "gopher-v2"); err != nil {
		log.Fatal(err)
	}
}

---
title: 第 65 讲 · database/sql、连接池与事务
description: 讲清驱动与 DSN、连接池四个关键参数、Query 与 Exec 的选择、事务与隔离级别、NULL 扫描，写出不漏连接不误用事务的数据层
---

<StageHeader stage="L6 · 服务端与数据" level="★★★☆☆" time="45 分钟" goal="写出不漏连接、不误用事务的数据层" prereq="第 64 讲 · WebSocket / SSE" />

# 第 65 讲 · database/sql、连接池与事务

> 数据层的事故往往不是「SQL 写错了」，而是「连接用错了」：`sql.Open` 放在 Handler 里、查完不关 `rows`、事务里混用 `db` 与 `tx`、把可空列扫进普通 `string` 然后线上连夜报 `converting NULL to string is unsupported`。这一讲把 `database/sql` 的心智模型摆正：`sql.DB` 从来不是一条连接，而是一个并发安全的连接池。看懂这一点，四个调优参数和一堆「连接耗尽」的线上问题就都解释得通了。

## 这一讲你会得到什么

<CheckList id="65-goal" title="本讲目标" :items='["理解 sql.DB 是连接池，以及 sql.Open 与 Ping 的分工","能配置 MaxOpenConns 等四个参数并说出各自解决什么问题","会按场景选择 Query / QueryRow / Exec，并正确关闭 rows","能用 BeginTx 写事务，理解隔离级别与 NULL 扫描"]' />

## 1. 先建立直觉

把数据库想成一家只有固定数量窗口的银行。每条物理连接是一个柜台，建连要握手、鉴权、分配会话，很贵；`sql.DB` 就是大厅里那套**排队 + 复用柜台的调度系统**。你来办业务（一次查询）时，它从空闲柜台里借一个给你，用完归还而不是拆掉；空闲柜台放太久会被回收，避免中间网络设备静默断连导致「借到一个死连接」。

<ConceptDiagram type="db-pool" title="sql.DB 管的是池：空闲、在用、等待队列与生命周期" caption="MaxOpenConns 是柜台上限，超出就进等待队列；ConnMaxLifetime 决定连接活多久必须换新的" />

这里最反直觉的一点：`sql.Open` **不建立任何连接**。它只解析 DSN、找到已注册的驱动、返回一个 `*sql.DB` 句柄。真正的 TCP 握手要等到第一次需要连接时（`Ping`、`Query`、`Exec`）才发生。所以「Open 成功」不代表数据库可连，启动探针必须显式调用 `PingContext`。同理，`sql.DB` 被设计成并发安全的长生命周期对象，整个进程复用一份即可。

## 2. 语法与规则

第一段代码是完整、可运行的 CRUD：配置连接池、用 `run()` 返回错误以保证 `defer` 生效、参数化查询、遍历结果并检查 `rows.Err()`。示例用纯 Go 的 SQLite 驱动，无需本机装数据库即可运行。

```go
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
```

<OutputBox title="运行结果" output="inserted id: 1\nquery row: {ID:1 Name:gopher Age:7}\nrow: {ID:1 Name:gopher Age:7}" note="SQLite 的 LastInsertId 与 AUTOINCREMENT 配合可用；MySQL 驱动同样返回自增主键" />

<LineByLine code="rows, err := db.QueryContext(ctx, query)\nif err != nil {\nreturn err\n}\ndefer rows.Close()\nfor rows.Next() {\nrows.Scan(dest)\n}\nreturn rows.Err()" :notes='["从池里借一条连接执行查询，返回 rows 游标","SQL 语法错、连接失败在这一步返回","检查错误后再注册关闭，避免在 nil rows 上 defer","必须关闭，否则连接不会归还池中","逐行推进游标，读完自动释放连接","把当前行列值扫进目标变量，个数与顺序要一致","循环结束仍要检查迭代期间的网络或解码错误"]' title="多行查询的标准骨架逐行看" />

三个入口怎么选，其实只看一件事：**会不会返回结果集**。`Query` 会，`Exec` 不会（只给 `LastInsertId` 与 `RowsAffected`），`QueryRow` 是「至多一行」的特例，且把 `sql.ErrNoRows` 作为错误返回。用 `Exec` 执行 SELECT 或反过来，轻则拿不到数据，重则让驱动一直持有结果集不释放。

第二段代码演示事务：`BeginTx`、超时、`defer Rollback` 与显式 `Commit`。

```go
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
```

有一个非常隐蔽的坑：事务期间**必须全程用 `tx`**，任何一句误写成 `db.ExecContext` 的语句都会去池里另借一条连接，它既看不到事务内的未提交修改，也不受回滚保护，直接破坏原子性。`BeginTx` 会为事务独占一条连接直到 `Commit` 或 `Rollback`，这也是长事务容易把池打满的原因——事务要尽量短，别在里面做 RPC 或文件 IO。

## 3. 深入一步

| 参数 | 默认值 | 含义 | 调优要点 |
| --- | --- | --- | --- |
| `SetMaxOpenConns` | 无限制 | 同时打开的连接总数上限 | 必须设，否则高并发下把数据库连接数打满；通常略大于 DB 的 `max_connections / 实例数` |
| `SetMaxIdleConns` | 2 | 池中保留的空闲连接数 | 设得太小会频繁建连；一般与 `MaxOpenConns` 同量级 |
| `SetConnMaxLifetime` | 永不过期 | 连接从创建起最长存活时间 | 应小于数据库或负载均衡器的空闲断连时间，避免借到死连接 |
| `SetConnMaxIdleTime` | 永不过期 | 空闲多久后被回收 | 低峰期主动缩池，释放数据库侧资源 |

`MaxOpenConns` 之外的请求不会失败，而是**阻塞在等待队列里**，直到有连接归还或 `ctx` 超时。所以压测时看到延迟毛刺而不是报错，第一反应就该去看 `db.Stats()` 里的 `WaitCount` 与 `WaitDuration`。

| 隔离级别 | 脏读 | 不可重复读 | 幻读 |
| --- | --- | --- | --- |
| Read Uncommitted | 可能 | 可能 | 可能 |
| Read Committed | 不会 | 可能 | 可能 |
| Repeatable Read | 不会 | 不会 | 可能 |
| Serializable | 不会 | 不会 | 不会 |

Go 用 `sql.LevelXxx` 常量表达隔离级别，最终由驱动翻译成 `SET TRANSACTION ISOLATION LEVEL ...`；并非所有驱动都支持全部级别（SQLite 就不支持），指定不支持的级别会直接报错。级别越高一致性越强、并发吞吐越低，默认级别通常已够用，别盲目上 Serializable。

<Term word="DSN" text="Data Source Name，驱动的连接描述串，包含地址、库名、认证与参数；格式由驱动自定义，不是 database/sql 统一规定的" />

<Callout type="warn" title="两个必须点名的反模式" text="其一，每次请求都 sql.Open。这会为每个请求新建一整套连接池，握手开销翻倍，旧池连接来不及释放，很快耗尽数据库连接数；sql.DB 应当作为进程级单例复用。其二，忽略 rows.Close。连接只有在 rows 关闭（或耗尽）后才归还池中，漏关一次就可能让 MaxOpenConns 被慢慢吃光，表现是服务越跑越慢直至全部阻塞。" />

<Callout type="idea" title="NULL 处理" text="可空列直接 Scan 到 string/int 会报 converting NULL to string is unsupported。两种解法：用 sql.NullString、sql.NullInt64、sql.NullTime 等包装类型并先判断 Valid；或在 SQL 里用 COALESCE(col, 默认值) 转成非空再扫进普通类型。前者保留「没填」与「填了空串」的区别，后者代码更简洁。" />

第三段代码把这两点放进一个完整、可运行的 HTTP 服务：进程级 `db` 单例 + Handler 里的 NULL 扫描与 `sql.ErrNoRows` 处理。

```go
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
	db, err = sql.Open("sqlite", "file:app.db?_pragma=busy_timeout(5000)")
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
```

## 4. 工程实践

把数据层接进服务时，有三个边界要守。第一，**context 一定从请求透传到查询**：用 `QueryRowContext(r.Context(), ...)`，客户端断开或超时，数据库查询才会随之取消，否则慢 SQL 会继续占着连接。第二，**所有 SQL 都参数化**，表名列名若必须动态拼接就用白名单，绝不把用户输入插进语句。第三，**把 `db` 收进结构体而不是散落全局**，但注意别把 `context.Context` 存进结构体字段长期持有——context 应当作为函数第一个参数显式传递（这一点在第 47 讲与第 61 讲都强调过）。

<Compare title="sql.DB 该活多久" :panels='[{"label":"反模式 · 每次请求都 Open","code":"func handler(w, r) {\ndb, _ := sql.Open(driver, dsn)\ndefer db.Close()\ndb.Query(query)\n}","note":"每次新建一整套连接池，握手开销翻倍，旧池连接来不及释放，很快打满数据库连接数"},{"label":"正确 · 进程级单例","code":"var db *sql.DB\nfunc init() {\ndb, _ = sql.Open(driver, dsn)\ndb.SetMaxOpenConns(25)\n}","note":"sql.DB 并发安全且自建池，全进程复用一份，由池统一负责复用、限流与生命周期"}]' />

<FlowTimeline title="一次查询的连接生命周期" :steps='[{"title":"借连接","desc":"池中有空闲则复用，否则新建或进等待队列","tone":"blue"},{"title":"执行 SQL","desc":"受 ctx 超时与取消约束","tone":"purple"},{"title":"遍历 rows","desc":"游标持有连接，逐行 Scan","tone":"orange"},{"title":"归还","desc":"rows.Close 或耗尽后连接回到池中","tone":"green"},{"title":"回收","desc":"超过 ConnMaxLifetime 或 MaxIdleTime 时物理关闭","tone":"gray"}]' direction="horizontal" />

线上排查连接问题先看 `db.Stats()`：`OpenConnections`、`InUse`、`Idle`、`WaitCount`。`WaitCount` 持续增长说明 `MaxOpenConns` 偏小或有连接没归还；`InUse` 长期贴近 `MaxOpenConns` 而吞吐上不去，多半是慢 SQL 或长事务。另一个反模式是**事务里混用 db 与 tx**，前文已说明它会绕过原子性，代码评审时值得专门盯一眼。

## 动手练一练

<Exercise title="写一个安全的转账事务" task="补全 transfer：从账户 1 扣 30、给账户 2 加 30，任一步失败都要整体回滚，并保证连接与事务一定被释放。" :hints='["用 db.BeginTx(ctx, nil) 拿事务，之后所有语句都调 tx 上的方法","紧跟 BeginTx 写 defer tx.Rollback()，Commit 之后它自然失效","两段 UPDATE 都要检查错误并直接 return"]' solution="func transfer(ctx context.Context, db *sql.DB) error {\ntx, err := db.BeginTx(ctx, nil)\nif err != nil {\nreturn err\n}\ndefer tx.Rollback()\nif _, err := tx.ExecContext(ctx, debit); err != nil {\nreturn err\n}\nif _, err := tx.ExecContext(ctx, credit); err != nil {\nreturn err\n}\nreturn tx.Commit()\n}" explain="事务期间连接被独占，所以函数要尽快返回；defer Rollback 是兜底，Commit 成功后它返回 ErrTxDone，忽略即可。" />

## 随堂检测

<Quiz no="65-1" q="关于 sql.Open，下列说法正确的是？" :options='["它会立即建立一条数据库连接","它只构造连接池，第一次 Ping/Query 时才建连","每个请求都应调用一次以保证隔离","返回的是单条连接，不是线程安全的"]' answer="1" explain="sql.Open 只解析 DSN 并注册驱动、返回并发安全的 *sql.DB 句柄；真正建连发生在首次需要连接时，因此启动探针要显式 PingContext。" />

<Predict no="65-2" q="Handler 里查询后忘记 rows.Close()，且 MaxOpenConns=25，持续请求一段时间会怎样？" :options='["报错 sql: no rows","连接逐渐不归还，最终所有请求阻塞在等待队列","自动被 GC 回收，没有影响","数据库主动断开空闲连接"]' answer="1" output="InUse 逐渐逼近 25，WaitCount 持续上涨，后续请求延迟飙升直至超时" explain="连接只在 rows 关闭或耗尽后归还池中，漏关会慢慢吃光池容量；db.Stats 的 WaitCount 是排查这类问题的第一现场。" />

## 一页纸速记

<KnowledgeCard :points='["sql.DB 是并发安全的连接池，进程级单例复用，Open 不建连、Ping 才建连","四个参数：MaxOpenConns 限总量、MaxIdleConns 留空闲、ConnMaxLifetime 与 ConnMaxIdleTime 管回收","Query 返回结果集、Exec 不返回、QueryRow 至多一行；多行查询必须 defer rows.Close 并检查 rows.Err","事务全程只用 tx，BeginTx 独占连接；可空列用 sql.NullXxx 或 COALESCE"]' :pitfalls='["每次请求 sql.Open，握手开销与连接数双双爆炸","忽略 rows.Close，连接慢慢被吃光","事务里误用 db 而不是 tx，破坏原子性","把 context 存进结构体字段长期持有","把用户输入直接拼进 SQL 字符串"]' summary="把 sql.DB 当成池而不是连接，连接生命周期、事务边界与 NULL 处理就都有了统一解释。" />

## 小结与下一讲

- `sql.DB` 是连接池，长生命周期、并发安全；`Open` 不建连，`PingContext` 才是启动探针。
- 四个池参数分别管总量、空闲与寿命；`db.Stats()` 是连接问题的第一诊断入口。
- 多行查询必须关 `rows` 并查 `rows.Err()`，事务全程只用 `tx`，可空列用 `sql.NullXxx`。

原生 `database/sql` 写多了，会希望有人把 Scan、模型映射、关联查询这些样板代码收掉。下一讲对比 GORM、sqlx 与 sqlc。

[下一讲：GORM / sqlx / sqlc 与迁移](/server/orm)

---
title: 第 065 讲 · database/sql、连接池与事务
description: 讲清驱动与 DSN、连接池四个关键参数、Query 与 Exec 的选择、事务与隔离级别、NULL 扫描，写出不漏连接不误用事务的数据层
---

<StageHeader stage="L6 · 服务端与数据" level="★★★☆☆" time="45 分钟" goal="写出不漏连接、不误用事务的数据层" prereq="第 064 讲 · WebSocket / SSE" />

# 第 065 讲 · database/sql、连接池与事务

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

### 协议与契约：占位符、NULL 与事务隔离的推导

**占位符为什么能从根上消除注入。** `database/sql` 把 SQL 文本与参数**分两条通道**交给驱动：文本走预处理语句（或 `QueryerContext`），参数以 `driver.Value` 单独绑定，值永远不参与 SQL 语法解析。推导：字符串拼接 `WHERE name = '` + 输入 + `'` 时，输入里的 `' OR 1=1 --` 会改变语句的语法树，让 WHERE 恒真；而 `QueryRowContext(ctx, "WHERE name = ?", 输入)` 里同一个字符串只是一个 VARCHAR 值，语法树保持不变。边界同样重要：占位符只能替**值**，不能替表名、列名、排序方向——`SELECT ? FROM users` 会把字符串当列值而不是列名，因为标识符在 SQL 里不是值。动态标识符必须走白名单映射。占位符风格（`?`、`$1`、sqlx 的 `:name`）与 DSN 格式都由**驱动**定义，不是 `database/sql` 统一规定的，换库时要重新确认。

**NULL 的坑都源于三值逻辑。** SQL 用 TRUE / FALSE / **UNKNOWN** 三值：`col = NULL` 的结果恒为 UNKNOWN，而 `WHERE` 只保留 TRUE，所以查空值必须写 `IS NULL` 而不是 `= NULL`；`NOT IN` 子查询里只要含一个 NULL，整条结果就变空；聚合函数大多忽略 NULL，但 `COUNT(*)` 不忽略。Go 的零值里没有「未知」这一态，于是 `Scan` 把 NULL 扫进 `*string` / `*int` 会报 `converting NULL to string is unsupported`。`sql.NullString` 用一个 `Valid` 位补上这一态，`COALESCE(col, 默认值)` 则在 SQL 侧把 UNKNOWN 折叠成确定值。推导结论：所有 NULL 事故，本质都是「两值语言的变量去接三值逻辑的结果」。

**隔离级别是现象定义，不是跨库承诺。** ANSI SQL-92 用三种异常现象（脏读、不可重复读、幻读）定义四级隔离；级别越高，锁范围越大、持有越久（或 MVCC 快照越保守），并发吞吐越低。Go 的 `sql.LevelXxx` 只是常量，最终由驱动翻译成 `SET TRANSACTION ISOLATION LEVEL ...`，并非所有驱动支持全部级别（SQLite 基本只支持默认与 serializable），指定不支持的会直接报错。实现差异更大：MySQL InnoDB 的 Repeatable Read 靠 MVCC 加间隙锁，实际能挡住大部分幻读；Postgres 的 Repeatable Read 用事务级快照，行为又不同。所以第 3 节那张表是「现象定义」，迁移数据库时必须重新验证，别把它当跨库保证。

**连接池耗尽可以用排队论推出来。** `MaxOpenConns` 之外的请求**不会报错，而是阻塞在等待队列**，直到有连接归还或 ctx 超时。按 Little's Law，并发数约等于吞吐乘以延迟：若一条慢 SQL 把平均延迟从 5ms 推到 500ms，维持同样 QPS 就需要约 100 倍并发连接，池会瞬间被借空。这就是为什么压测里先看到的是延迟毛刺而不是错误码，第一诊断现场应是 `db.Stats()` 的 `WaitCount` 与 `WaitDuration`，而不是应用日志里的报错。

### 设计取舍与替代方案

| 决策点 | 方案 A | 方案 B（推荐） | 方案 A 的代价 |
| --- | --- | --- | --- |
| 动态 SQL | 字符串拼接 | 值用占位符，标识符走白名单 | 拼接改变语法树，注入与全表扫描风险 |
| 可空列 | 扫进普通 `string` / `int` | `sql.NullXxx` 保留未知，或 `COALESCE` 折叠 | 遇到 NULL 直接报错或悄悄把「没填」当成空串 |
| 隔离级别 | 盲目上 Serializable | 用默认级别，按需局部提升 | 锁竞争加剧，吞吐显著下降 |
| 池容量 | 不设上限或拍很大 | 略大于 `max_connections / 实例数` | 把连接数打满数据库，或只是把瓶颈挪到 DB |
| 事务边界 | 事务里做 RPC / 文件 IO | 事务尽量短，只包数据库操作 | `BeginTx` 独占连接，长事务把池打满 |

### 失败矩阵

| 失败场景 | 触发条件 | 可观测现象 | 定位手段 | 对策 |
| --- | --- | --- | --- | --- |
| 连接池耗尽 | 漏 `rows.Close()`、长事务、慢 SQL | `WaitCount` 上涨、延迟毛刺 | `db.Stats()` | 显式关闭、短事务、超时 |
| 慢查询 | 缺索引或深分页 | `InUse` 贴近上限，吞吐上不去 | `EXPLAIN`、慢 SQL 日志 | 补索引、改游标分页 |
| 事务失原子 | 事务里误用 `db` 而非 `tx` | 部分写入不受回滚保护 | 代码评审、SQL 日志 | 全程只用 `tx` |
| NULL 崩溃 | 可空列扫进普通类型 | `converting NULL` 报错 | 错误日志 | `sql.NullXxx` 或 `COALESCE` |
| 注入 | 输入拼进 SQL | 数据泄露或全表被拖 | SQL 审计日志 | 参数化加白名单 |
| 占位符误用 | 把表名当参数绑定 | 语法错误或类型错误 | 驱动报错信息 | 标识符白名单 |
| 死连接 | `ConnMaxLifetime` 大于中间设备空闲断连 | 借到半开连接，偶发 EOF | 连接存活时长 | 缩短 lifetime 加读探测 |
| 取消未透传 | 用 `Query` 而非 `QueryContext` | 客户端已断，慢查询仍占连接 | SQL 慢日志 | 全程透传 ctx |
| 重试放大 | 下游超时被无限重试 | 池等待队列越排越长 | 入口与 DB QPS 对比 | 退避加重试预算 |

<SequenceDiagram title="借连接、事务独占与取消传播" :actors='["Handler","sql.DB","Pool","Driver","DB"]' :steps='[{"from":"Handler","to":"sql.DB","label":"QueryContext","note":"带 r.Context 与占位符","kind":"call"},{"from":"sql.DB","to":"Pool","label":"借连接","note":"池空则排队","kind":"call"},{"from":"Pool","to":"Driver","label":"绑定参数","note":"值与 SQL 分通道","kind":"call"},{"from":"Driver","to":"DB","label":"发送语句","note":"预处理加参数","kind":"call"},{"from":"DB","to":"Driver","label":"返回游标","note":"结果集","kind":"return"},{"from":"Driver","to":"Handler","label":"逐行 Scan","note":"NULL 需 NullXxx","kind":"return"},{"from":"Handler","to":"Handler","label":"rows.Close","note":"连接才归还","kind":"self"},{"from":"Handler","to":"sql.DB","label":"BeginTx","note":"独占一条连接","kind":"call"},{"from":"sql.DB","to":"Pool","label":"独占","note":"直到 Commit 或 Rollback","kind":"call"},{"from":"Handler","to":"Handler","label":"误用 db","note":"另借连接破坏原子性","kind":"self"},{"from":"Handler","to":"sql.DB","label":"Commit","note":"释放独占","kind":"call"},{"from":"Pool","to":"DB","label":"ctx 取消","note":"驱动取消在途 SQL","kind":"async"}]' />

## 4. 工程实践

### 可验证实验：把执行计划与 NULL 契约跑出来看

**实验一：EXPLAIN QUERY PLAN 看索引与占位符。** 用第 2 节的 SQLite 库，对比加索引前后同一个查询的执行计划：

```bash
sqlite3 app.db "EXPLAIN QUERY PLAN SELECT * FROM users WHERE email = 'a@b.com';"
sqlite3 app.db "CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);"
sqlite3 app.db "EXPLAIN QUERY PLAN SELECT * FROM users WHERE email = 'a@b.com';"
```

<OutputBox title="加索引前后的执行计划" output="第 1 次: QUERY PLAN\n`--SCAN users\n第 2 次: QUERY PLAN\n`--SEARCH users USING INDEX idx_users_email (email=?)" note="SCAN 是全表扫描，SEARCH 是索引定位" />

逐字段解释：`QUERY PLAN` 是结果集头；`SCAN users` 表示对 users 做全表逐行扫描，代价 O(N)，用户表一大就会变成慢查询并占住连接；`SEARCH users USING INDEX idx_users_email` 表示走索引定位，代价近似 O(log N)；`(email=?)` 里的 `?` 正是参数绑定占位符在执行计划里的样子——它说明这条查询用过参数化，而不是把字面量拼进 SQL。把这一步和「连接池耗尽」连起来：一次 `SCAN` 若从 5ms 变 500ms，按排队论并发需求放大约 100 倍，池立刻被打满。生产里对应 MySQL 的 `EXPLAIN`，重点看 `type`、`rows`、`Extra` 三列。

**实验二：sqlmock 加 go test -race 断言 NULL 与参数绑定。** 下面这段完整可编译的测试文件用 `github.com/DATA-DOG/go-sqlmock` 把「NULL 不能扫进普通 string」「`sql.NullString` 能表达未知」「参数绑定把值单独交给驱动」三件事变成可执行断言：

```go
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
```

```bash
go test -race -run TestNullScanAndQueryContract -v ./...
```

<OutputBox title="go test -race 预期输出" output="=== RUN   TestNullScanAndQueryContract\n--- PASS: TestNullScanAndQueryContract (0.00s)\nPASS\nok   example.com/server   1.312s" note="三个断言全过，且 race 检测无告警" />

逐字段解释：第一个断言要求 `Scan(&name, &email)` **必须报错**，若它成功说明驱动把 NULL 悄悄转成了空串、三值语义被吞掉；第二个断言要求 `sql.NullString` 成功且 `Valid=false`，证明「未知」这一态被显式保留；`ExpectExec` 里用正则转义了括号与 `?`，`WithArgs` 单独声明期望的参数值，这正是「SQL 文本与参数分两条通道」的可验证形式——把 `WithArgs` 改成错误值，用例立即失败，说明参数确实是独立传下去的。`ExpectationsWereMet` 确认没有遗漏或多余的 SQL；`-race` 无告警说明 mock 与断言间无共享状态竞争。把 `sql.NullString` 换回普通 `string` 再跑，第一处 `t.Fatal` 就会触发，NULL 契约一目了然。

<Predict q="执行 SELECT * FROM users WHERE deleted_at = NULL 想查出未删除的用户，结果如何？" :options='["正常返回未删除的用户","一条都查不到","报语法错误","返回全部用户"]' answer="1" output="一条都查不到，因为 deleted_at = NULL 的结果是 UNKNOWN，WHERE 只保留 TRUE" explain="SQL 是三值逻辑，与 NULL 的等值比较恒为 UNKNOWN；查空值必须用 IS NULL。NOT IN 子查询含 NULL 时同理会让结果集为空。" />

### 两条真实工程误解

**误解一：`WHERE email = NULL` 能查出没填邮箱的用户。** 错。三值逻辑下 `= NULL` 恒为 UNKNOWN，`WHERE` 不会保留，结果必然是空集；正确写法是 `IS NULL` 或 `IS NOT NULL`。更隐蔽的是 `id NOT IN (SELECT parent_id FROM t)`，只要子查询里出现一个 NULL，整个条件就变 UNKNOWN、返回零行。这类 bug 不会报错，只会「数据莫明变少」，靠单测最难发现，必须靠对三值逻辑的理解提前避开。

**误解二：占位符能参数化一切，把表名也用 `?` 传进去更省事。** 错。SQL 标识符（表名、列名、`ASC/DESC`）不是值，不能绑定；`SELECT ? FROM users` 会把参数当成一个字面量列，返回一堆常量或类型错误。真实业务里「按租户分表」「按字段排序」又确实需要动态标识符，正解是服务端维护一张白名单，把用户输入映射成有限几个合法标识符，再拼进 SQL 的标识符位置，值部分继续用占位符。把「参数化」当成万能，反而会在需要动态标识符的地方写出注入漏洞。

### 与后续阶段的呼应

- 本讲的 ctx 透传与连接池参数，在 /microservice/grpc-advanced 会升级为跨服务 Deadline 传播；在 /microservice/resilience 里由超时、重试预算与熔断决定「下游慢时还该不该继续放大查询」；/microservice/metrics 则把 `WaitCount`、`InUse`、`WaitDuration` 做成可告警曲线。
- 占位符、NULL 与隔离级别在 /server/orm 会被 ORM 重新包装一遍，但底层仍是同一套 `database/sql` 契约；/distributed/distributed-tx 处理跨库事务时，本讲的本地事务与 ACID 是理解 2PC / TCC / Saga 的起点。
- L10 的 /projects/net-shortlink 读路径几乎全是单行主键查询，正是「短事务加连接池有上限」的典型受益者；/projects/data-sql-tool 要把 `EXPLAIN` 的执行计划做成通用查询工具，/projects/micro-ecommerce 的订单与库存则把事务边界当成正确性底线。

把数据层接进服务时，有三个边界要守。第一，**context 一定从请求透传到查询**：用 `QueryRowContext(r.Context(), ...)`，客户端断开或超时，数据库查询才会随之取消，否则慢 SQL 会继续占着连接。第二，**所有 SQL 都参数化**，表名列名若必须动态拼接就用白名单，绝不把用户输入插进语句。第三，**把 `db` 收进结构体而不是散落全局**，但注意别把 `context.Context` 存进结构体字段长期持有——context 应当作为函数第一个参数显式传递（这一点在第 047 讲与第 061 讲都强调过）。

<Compare title="sql.DB 该活多久" :panels='[{"label":"反模式 · 每次请求都 Open","code":"func handler(w, r) {\ndb, _ := sql.Open(driver, dsn)\ndefer db.Close()\ndb.Query(query)\n}","note":"每次新建一整套连接池，握手开销翻倍，旧池连接来不及释放，很快打满数据库连接数"},{"label":"正确 · 进程级单例","code":"var db *sql.DB\nfunc init() {\ndb, _ = sql.Open(driver, dsn)\ndb.SetMaxOpenConns(25)\n}","note":"sql.DB 并发安全且自建池，全进程复用一份，由池统一负责复用、限流与生命周期"}]' />

<FlowTimeline title="一次查询的连接生命周期" :steps='[{"title":"借连接","desc":"池中有空闲则复用，否则新建或进等待队列","tone":"blue"},{"title":"执行 SQL","desc":"受 ctx 超时与取消约束","tone":"purple"},{"title":"遍历 rows","desc":"游标持有连接，逐行 Scan","tone":"orange"},{"title":"归还","desc":"rows.Close 或耗尽后连接回到池中","tone":"green"},{"title":"回收","desc":"超过 ConnMaxLifetime 或 MaxIdleTime 时物理关闭","tone":"gray"}]' direction="horizontal" />

线上排查连接问题先看 `db.Stats()`：`OpenConnections`、`InUse`、`Idle`、`WaitCount`。`WaitCount` 持续增长说明 `MaxOpenConns` 偏小或有连接没归还；`InUse` 长期贴近 `MaxOpenConns` 而吞吐上不去，多半是慢 SQL 或长事务。另一个反模式是**事务里混用 db 与 tx**，前文已说明它会绕过原子性，代码评审时值得专门盯一眼。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l6-server/lesson-065-database-sql`](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-065-database-sql)。clone 仓库后进入 `examples/l6-server`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="写一个安全的转账事务" task="补全 transfer：从账户 1 扣 30、给账户 2 加 30，任一步失败都要整体回滚，并保证连接与事务一定被释放。" :hints='["用 db.BeginTx(ctx, nil) 拿事务，之后所有语句都调 tx 上的方法","紧跟 BeginTx 写 defer tx.Rollback()，Commit 之后它自然失效","两段 UPDATE 都要检查错误并直接 return"]' solution="func transfer(ctx context.Context, db *sql.DB) error {\ntx, err := db.BeginTx(ctx, nil)\nif err != nil {\nreturn err\n}\ndefer tx.Rollback()\nif _, err := tx.ExecContext(ctx, debit); err != nil {\nreturn err\n}\nif _, err := tx.ExecContext(ctx, credit); err != nil {\nreturn err\n}\nreturn tx.Commit()\n}" explain="事务期间连接被独占，所以函数要尽快返回；defer Rollback 是兜底，Commit 成功后它返回 ErrTxDone，忽略即可。" />

## 随堂检测

<Quiz no="065-1" q="关于 sql.Open，下列说法正确的是？" :options='["它会立即建立一条数据库连接","它只构造连接池，第一次 Ping/Query 时才建连","每个请求都应调用一次以保证隔离","返回的是单条连接，不是线程安全的"]' answer="1" explain="sql.Open 只解析 DSN 并注册驱动、返回并发安全的 *sql.DB 句柄；真正建连发生在首次需要连接时，因此启动探针要显式 PingContext。" />

<Predict no="065-2" q="Handler 里查询后忘记 rows.Close()，且 MaxOpenConns=25，持续请求一段时间会怎样？" :options='["报错 sql: no rows","连接逐渐不归还，最终所有请求阻塞在等待队列","自动被 GC 回收，没有影响","数据库主动断开空闲连接"]' answer="1" output="InUse 逐渐逼近 25，WaitCount 持续上涨，后续请求延迟飙升直至超时" explain="连接只在 rows 关闭或耗尽后归还池中，漏关会慢慢吃光池容量；db.Stats 的 WaitCount 是排查这类问题的第一现场。" />

## 一页纸速记

<KnowledgeCard :points='["sql.DB 是并发安全的连接池，进程级单例复用，Open 不建连、Ping 才建连","四个参数：MaxOpenConns 限总量、MaxIdleConns 留空闲、ConnMaxLifetime 与 ConnMaxIdleTime 管回收","Query 返回结果集、Exec 不返回、QueryRow 至多一行；多行查询必须 defer rows.Close 并检查 rows.Err","事务全程只用 tx，BeginTx 独占连接；可空列用 sql.NullXxx 或 COALESCE"]' :pitfalls='["每次请求 sql.Open，握手开销与连接数双双爆炸","忽略 rows.Close，连接慢慢被吃光","事务里误用 db 而不是 tx，破坏原子性","把 context 存进结构体字段长期持有","把用户输入直接拼进 SQL 字符串"]' summary="把 sql.DB 当成池而不是连接，连接生命周期、事务边界与 NULL 处理就都有了统一解释。" />

## 小结与下一讲

- `sql.DB` 是连接池，长生命周期、并发安全；`Open` 不建连，`PingContext` 才是启动探针。
- 四个池参数分别管总量、空闲与寿命；`db.Stats()` 是连接问题的第一诊断入口。
- 多行查询必须关 `rows` 并查 `rows.Err()`，事务全程只用 `tx`，可空列用 `sql.NullXxx`。

原生 `database/sql` 写多了，会希望有人把 Scan、模型映射、关联查询这些样板代码收掉。下一讲对比 GORM、sqlx 与 sqlc。

[下一讲：GORM / sqlx / sqlc 与迁移](/server/orm)

---
title: 第 066 讲 · GORM / sqlx / sqlc 与迁移
description: 对比 GORM、sqlx、sqlc 的取舍，讲清模型与关联、预加载与 N+1、版本化迁移和 SQL 可观测性，在 ORM 与原生 SQL 之间做理性选择
---

<StageHeader stage="L6 · 服务端与数据" level="★★★☆☆" time="45 分钟" goal="在 ORM 与原生 SQL 之间做理性选择" prereq="第 065 讲 · database/sql、连接池与事务" />

# 第 066 讲 · GORM / sqlx / sqlc 与迁移

> 上一讲的 `rows.Scan` 写十遍之后，你一定会想找工具把结构体与表映射起来。但「ORM」不是一个开关，而是一整条光谱：GORM 帮你把对象关系全包了，sqlx 只把 `Scan` 这步自动化，sqlc 干脆把 SQL 在编译期变成类型安全的函数。它们对 SQL 的控制力、类型安全程度与运行时开销完全不同。这一讲用同一份业务对比三者，并解决一个 ORM 最经典的线上杀手：N+1 查询。

## 这一讲你会得到什么

<CheckList id="66-goal" title="本讲目标" :items='["能说清 GORM、sqlx、sqlc 各自把哪一层抽象掉了","会定义模型与关联，并用 Preload 消除 N+1 查询","能写出可重复执行、带版本记录的迁移","知道怎么打开 SQL 日志、定位慢查询与评估 RowsAffected"]' />

## 1. 先建立直觉

把数据访问想成「下馆子」。原生 `database/sql` 是自己去菜场买菜、洗切、掌勺，全流程可控但每一步都得自己干；sqlx 相当于买了净菜，锅铲还是你的，只是省掉切配；GORM 是直接点套餐，连菜谱都替你写好，但想改口味得照它的规矩来；sqlc 则像提前把菜谱交给厨房做成半成品，上桌前编译器已经帮你检查过食材对不对。

抽象越多，越要问两个问题：**它替我生成了什么 SQL**，以及**出问题时我能不能看懂、能不能改**。ORM 让 CRUD 变快，但也很容易让一条列表接口悄悄发出几百条 SQL。

<ConceptDiagram type="db-pool" title="N+1 查询如何把连接池打满" caption="循环里逐条查关联，SQL 条数随结果行数线性增长；每条查询都要从池里借连接，并发一上来等待队列立刻堆积" />

## 2. 语法与规则

第一段代码是完整、可运行的 GORM 服务，故意把「慢」与「快」两个 Handler 并排注册，方便对比关联加载的两种写法。

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type User struct {
	ID     uint    `gorm:"primaryKey"`
	Name   string  `gorm:"size:64;not null"`
	Orders []Order `gorm:"foreignKey:UserID"`
}

type Order struct {
	ID     uint `gorm:"primaryKey"`
	UserID uint `gorm:"index"`
	Amount int
}

func main() {
	// LogMode(logger.Warn) 只打慢查询与错误；开发期可临时开 logger.Info 看全量 SQL。
	db, err := gorm.Open(sqlite.Open("file:app.db?_pragma=busy_timeout(5000)"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		log.Fatal(err)
	}
	// AutoMigrate 只适合开发与测试；生产用显式版本化迁移（见第 3 段）。
	if err := db.AutoMigrate(&User{}, &Order{}); err != nil {
		log.Fatal(err)
	}
	seed(db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/slow", slowList(db))
	mux.HandleFunc("GET /users/fast", fastList(db))
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func seed(db *gorm.DB) {
	var n int64
	db.Model(&User{}).Count(&n)
	if n > 0 {
		return // 避免每次启动都重复插入
	}
	for i := 1; i <= 3; i++ {
		u := User{Name: fmt.Sprintf("user-%d", i),
			Orders: []Order{{Amount: i * 10}}}
		db.Create(&u)
	}
}

// slowList 演示 N+1：查完用户后，在循环里逐个查订单。
func slowList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var users []User
		db.WithContext(r.Context()).Find(&users)
		for i := range users {
			db.WithContext(r.Context()).
				Model(&users[i]).Association("Orders").Find(&users[i].Orders)
		}
		writeJSON(w, users)
	}
}

// fastList 用 Preload 一次 IN 查询取回全部关联，SQL 条数固定为 2。
func fastList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var users []User
		db.WithContext(r.Context()).Preload("Orders").Find(&users)
		writeJSON(w, users)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
```

<OutputBox title="打开全量 SQL 日志后的对比" output="N+1 写法：SELECT * FROM users\n随后按用户数逐条 SELECT * FROM orders WHERE user_id = ?\n共 1 + N 条 SQL\n\nPreload 写法：SELECT * FROM users\nSELECT * FROM orders WHERE user_id IN (1,2,3)\n固定 2 条 SQL" note="把 logger 临时调到 Info 是定位 N+1 最快的办法；慢接口先看 SQL 条数，再看单条耗时" />

<LineByLine code="var users []User\nerr := db.WithContext(ctx).Preload(orders).Find(users).Error\nif err != nil {\nreturn err\n}\nreturn writeJSON(w, users)" :notes='["声明结果切片，GORM 按字段名与表列名做映射","WithContext 把请求取消与超时透传到查询，Preload 声明要一起加载的关联","检查 Error，GORM 的链式调用把错误累积在结果对象里","失败立即返回，不要带着半成品继续写响应","成功才序列化返回"]' title="预加载查询逐行看" />

第二段代码换到 sqlx：它不生成 SQL、不管理关联，只把 `rows.Scan` 自动化，因此 SQL 始终完全可见。下面同样是完整服务，含列表、详情与命名参数写入。

```go
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
```

`sqlx` 的定位很清楚：它只是把 `database/sql` 的结果集映射自动化，没有会话缓存、没有脏检查、没有隐式 SQL。当你写的是报表、批量更新这类复杂查询，又嫌 `Scan` 啰嗦，它往往比全功能 ORM 更顺手。

## 3. 深入一步

第三种思路 sqlc 不运行时反射，而是**从你写的 SQL 生成 Go 代码**：先在 `.sql` 文件里写查询并标注函数名，`sqlc generate` 产出带强类型参数与返回值的函数。好处是 SQL 写得再复杂也能在编译期检查列名与类型，没有 ORM 的运行时开销，坏处是多了一步代码生成、动态条件支持较弱。

| 维度 | GORM | sqlx | sqlc |
| --- | --- | --- | --- |
| 抽象层次 | 全功能 ORM | `database/sql` 薄封装 | 编译期代码生成 |
| SQL 由谁写 | 框架生成 | 开发者写 | 开发者写、工具生成代码 |
| 类型安全 | 运行时 | 运行时 | 编译期 |
| 关联与预加载 | 内置 | 自己写 JOIN / IN | 自己写 SQL |
| 迁移 | AutoMigrate | 需外部工具 | 需外部工具 |
| 适合场景 | 后台管理、快速 CRUD | 复杂查询、渐进改造 | 强契约、高要求的查询层 |

<Term word="N+1 查询" text="先查一批主记录（1 条 SQL），再对每条记录单独查关联（N 条 SQL）；结果行数越多 SQL 越多，是 ORM 最常见的性能反模式" />

<Term word="预加载" text="用一条额外的 IN 查询把整批关联一次取回并在内存中拼装，把 1+N 条 SQL 压成固定 2 条" />

<Callout type="warn" title="反模式：AutoMigrate 上生产" text="AutoMigrate 只会加表、加列、加索引，从不删列、不改类型，且在多实例同时启动时可能并发执行 DDL。生产环境应使用版本化迁移文件（如 golang-migrate、goose），在发布流程里显式执行、可审查可回滚。把它当开发期便利工具就好。" />

<Callout type="idea" title="SQL 可观测性" text="三件小事能省下大量排查时间：开发期把 GORM logger 调到 Info 看全量 SQL；线上开慢查询阈值并记录耗时与调用栈；写路径检查 RowsAffected 是否为 0（更新条件没命中往往是 bug 而非成功）。所有查询都用 WithContext 或 XxxContext 透传请求 context，客户端断开时查询才会跟着取消。" />

第三段代码用零依赖的 `database/sql` 实现一个最小但可用的版本化迁移器，说明迁移工具背后的原理：版本表 + 事务 + 幂等。

```go
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
```

关键在「DDL 与版本记录放在同一个事务里」：任何一步失败都整体回滚，不会出现表改了但版本没记、下次重复执行炸掉的状态。真实工具还会补上 down 回滚脚本、锁表防并发、校验和防篡改，但骨架就是这些。

### 协议与契约：N+1、预加载与迁移的推导

**N+1 的成本可以精确算出来。** 一次列表返回 N 条主记录、每条再单独查一次关联，SQL 条数就是 `1 + N`，每条都要从池里借连接；并发 C 个请求时瞬时连接需求约为 `C × (1 + N)`。取 N=200、C=50，就是一万余条 SQL 去抢 `MaxOpenConns=25` 的连接，等待队列瞬间堆满、延迟飙升——第 2 节 Predict 的输出正来自这个乘积。`Preload` 把关联压成一条 `IN (...)` 查询后 SQL 条数固定为 2，与 N 无关，乘积里的 N 被消掉。为什么 ORM 特别容易触发它？因为在对象模型里「访问 `user.Orders`」只是一次属性读取，惰性加载把它悄悄翻译成一条 SQL；代码看上去没有循环查询，SQL 却按结果行数放大。所以定位手段不是读代码，而是打开全量 SQL 日志**先数条数，再看单条耗时**。

**但 Preload 不是绝对最优。** 当 ID 列表大到几千，`IN` 列表本身会让 SQL 变长、解析变慢，且所有关联行都要拉回内存拼装；关联数据量很大时，一条精心写的 JOIN 或分两阶段手动查询可能更省内存与传输。`SELECT *` 同样有契约问题：SQL 标准不保证列顺序，表一旦加列，按位置 `Scan` 的代码直接错位报错，按列名映射的 ORM 则会悄悄多出字段、多传数据。推导结论：显式列字段是把表结构当成对外契约锁住，而不是偷懒的样板代码。

**迁移的原子性有平台边界。** 第 3 节说「DDL 与版本记录放同一事务」，在 PostgreSQL 上成立（DDL 可事务化），但在 MySQL 上不成立：`ALTER TABLE`、`CREATE INDEX` 等 DDL 会触发**隐式提交**，把此前的事务先提交掉，于是「表改了、版本记录回滚」或「版本记了、DDL 失败」的状态仍可能出现。MySQL 上的正解是用 advisory lock 保证单实例执行，配合版本表与失败检测，发布流水线里只跑一次；SQLite 则连并发 DDL 都要靠外部串行化。AutoMigrate 在生产里还有第二宗罪：多实例同时启动时并发执行 DDL。理解「DDL 是否可事务」这个平台差异，才知道最小迁移器只是一个骨架。

**sqlc 把类型检查提前到编译期。** GORM 的映射靠运行时反射，列名或类型写错要到请求进来才报；sqlc 从 `.sql` 生成带强类型参数与返回值的 Go 函数，表结构改了却不重新生成，编译期就会失败——这正是「契约失配要尽早暴露」的收益。代价是多一步 `sqlc generate`、动态条件支持弱、SQL 方言与工具链绑定。三种工具不是「谁更好」，而是让渡多少 SQL 控制权：GORM 让渡最多换开发速度，sqlc 几乎不让渡换编译期保证。

### 设计取舍与替代方案

| 决策点 | 方案 A | 方案 B（视场景） | 方案 A 的代价 |
| --- | --- | --- | --- |
| 关联加载 | 循环里逐条查（N+1） | `Preload` 或手写 `IN`，数据量大时改 JOIN | SQL 条数随行数放大，池等待暴涨 |
| 迁移执行 | `AutoMigrate` 上生产 | 版本化迁移文件，单实例执行 | 不删列不改类型、并发 DDL、不可审查回滚 |
| 查询列 | `SELECT *` | 显式列出字段 | 表加列后映射变化、多传数据、按位置 Scan 错位 |
| 映射方式 | 运行时反射（GORM） | 编译期生成（sqlc） | 类型与列名错误延后到运行时 |
| 结果映射 | 手写 `rows.Scan` | sqlx 自动化 `StructScan` | 样板代码多、易漏列，但 SQL 完全可见 |
| 迁移原子性 | 假设所有库都支持事务化 DDL | 按库适配：Postgres 用事务，MySQL 用锁加版本表 | MySQL DDL 隐式提交，状态可能半途不一致 |

### 失败矩阵

| 失败场景 | 触发条件 | 可观测现象 | 定位手段 | 对策 |
| --- | --- | --- | --- | --- |
| N+1 | 循环里查关联 | SQL 条数随行数放大，`WaitCount` 暴涨 | 全量 SQL 日志数条数 | `Preload` 或一条 `IN` |
| 深分页 | offset 极大 | 单请求扫描百万行、连接被占住 | `EXPLAIN`、慢 SQL 日志 | 游标分页、limit 设上限 |
| 迁移半途失败 | DDL 与版本记录不同事务 | 重启后重复 DDL 报错 | 版本表与表结构比对 | 两者同事务，MySQL 改用锁 |
| 多实例并发 DDL | 滚动发布时多个实例同时迁移 | 表锁竞争、迁移报错 | 发布日志 | 流水线单实例执行 |
| `SELECT *` 后加列 | DDL 新增列 | 结构体映射变化、带宽上升 | 结果集列数监控 | 显式列字段 |
| 更新 0 行当成功 | 忽略 `RowsAffected` | 条件没命中却返回成功 | 返回行数断言 | 检查受影响行数 |
| 取消未透传 | `*gorm.DB` 不带 `WithContext` | 客户端断开查询仍在跑 | SQL 慢日志 | 全程透传 ctx |
| 连接池耗尽 | N+1 叠加慢查询 | 延迟毛刺而非报错 | `db.Stats()` | 固定 SQL 条数、超时 |

<SequenceDiagram title="N+1 如何把连接池借空" :actors='["Handler","ORM","Pool","DB"]' :steps='[{"from":"Handler","to":"ORM","label":"Find 列表","note":"1 条主查询","kind":"call"},{"from":"ORM","to":"Pool","label":"借连接","note":"执行主表查询","kind":"call"},{"from":"Pool","to":"DB","label":"SELECT 主表","note":"返回 N 行","kind":"call"},{"from":"DB","to":"ORM","label":"返回 N 行","note":"结果集","kind":"return"},{"from":"Handler","to":"ORM","label":"循环取关联","note":"共 N 次","kind":"call"},{"from":"ORM","to":"Pool","label":"借连接 1","note":"关联 1","kind":"call"},{"from":"Pool","to":"DB","label":"SELECT 关联 1","note":"按 user_id","kind":"call"},{"from":"DB","to":"ORM","label":"返回","note":"结果回传","kind":"return"},{"from":"ORM","to":"Pool","label":"借连接 N","note":"关联 N","kind":"call"},{"from":"Pool","to":"DB","label":"SELECT 关联 N","note":"累计 1+N 条","kind":"call"},{"from":"DB","to":"ORM","label":"返回","note":"结果回传","kind":"return"},{"from":"Pool","to":"Handler","label":"队列上涨","note":"后续请求排队","kind":"async"}]' />

## 4. 工程实践

### 可验证实验：把 N+1 的 SQL 条数与执行计划跑出来看

**实验一：EXPLAIN 看 `IN` 与全表扫描的差别。** 用第 2 节的 SQLite 库，对比关联查询的两种形态：

```bash
sqlite3 app.db "EXPLAIN QUERY PLAN SELECT * FROM orders WHERE user_id IN (1,2,3);"
sqlite3 app.db "CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);"
sqlite3 app.db "EXPLAIN QUERY PLAN SELECT * FROM orders WHERE user_id IN (1,2,3);"
```

<OutputBox title="加索引前后的关联查询计划" output="第 1 次: QUERY PLAN\n`--SCAN orders\n第 2 次: QUERY PLAN\n`--SEARCH orders USING INDEX idx_orders_user_id (user_id=?)" note="预加载的 IN 查询有没有走索引，决定它是 2 条快 SQL 还是 2 条慢 SQL" />

逐字段解释：`SCAN orders` 是全表扫描，即使 SQL 条数已经从 `1+N` 压到 2，单条仍是 O(N) 的慢查询，照样占住连接；`SEARCH ... USING INDEX idx_orders_user_id` 表示 `IN` 条件走了索引，代价降为对数级；`(user_id=?)` 是参数绑定的占位形态。这条实验补上了前一讲没说透的一层：**固定 SQL 条数只是第一步，每条 SQL 还要能走索引**。把索引删掉再压测，会看到 `WaitCount` 重新上涨——条数与单条耗时必须一起优化。

**实验二：用一个可编译的小测试数出 N+1 与预加载的调用次数。** 下面这段纯标准库测试把「循环写法发出 1+N 次查询、预加载固定 2 次」变成断言，不依赖任何 ORM：

```go
package main

import "testing"

type fakeDB struct{ queries int }

func (d *fakeDB) listUsers() []int {
	d.queries++
	return []int{1, 2, 3, 4, 5}
}

func (d *fakeDB) listOrders(userID int) {
	d.queries++
}

func (d *fakeDB) listOrdersIn(ids []int) {
	d.queries++
}

func slow(d *fakeDB) {
	for _, id := range d.listUsers() {
		d.listOrders(id)
	}
}

func fast(d *fakeDB) {
	ids := d.listUsers()
	d.listOrdersIn(ids)
}

func TestNPlusOneVsPreload(t *testing.T) {
	slowDB, fastDB := &fakeDB{}, &fakeDB{}
	slow(slowDB)
	fast(fastDB)
	if slowDB.queries != 6 {
		t.Fatalf("循环写法 SQL=%d，应为 1+5=6", slowDB.queries)
	}
	if fastDB.queries != 2 {
		t.Fatalf("预加载 SQL=%d，应为固定 2", fastDB.queries)
	}
}
```

```bash
go test -race -run TestNPlusOneVsPreload -v ./...
```

<OutputBox title="go test -race 预期输出" output="=== RUN   TestNPlusOneVsPreload\n--- PASS: TestNPlusOneVsPreload (0.00s)\nPASS\nok   example.com/server   1.208s" note="5 个用户时循环写法 6 次、预加载 2 次，且无数据竞争" />

逐字段解释：`--- PASS` 说明 `slowDB.queries == 6` 与 `fastDB.queries == 2` 同时成立，把「SQL 条数与行数无关」从结论变成可验证断言；把 `listUsers()` 返回的切片改成 200 个元素再跑，循环写法会变成 201 而预加载仍是 2，直接复现第 2 节 Predict 里「连接池被打满」的量级；`-race` 无告警说明计数器只在单 goroutine 里递增。对照实验：把 `fast` 里的 `listOrdersIn(ids)` 改回循环调用，第二个断言立即失败——N+1 的根因就是「把一次集合查询写成了逐条查询」。真实项目里把 `fakeDB.queries` 换成 GORM 的 Info 日志行数，结论完全一致。

<Predict q="表 users 新增一列后，一个一直用 SELECT * 并按列位置 Scan 的旧接口会怎样？" :options='["自动忽略新列，行为不变","Scan 参数个数或顺序错位而报错","编译期就报错","自动映射到新字段"]' answer="1" output="结果集列数变多，按位置 Scan 的目标变量个数不匹配，运行时报 sql: expected N destination arguments" explain="SQL 标准不保证列顺序，SELECT * 让表结构变化直接穿透到接口契约；显式列字段能把这层变化挡住。" />

### 两条真实工程误解

**误解一：`Preload` 一定比手写 JOIN 快。** 错。`Preload` 是「主表一条 + `IN` 一条 + 内存拼装」，它解决的是**SQL 条数固定**，不是绝对最快。当 ID 列表上千、或关联行数远大于主表行数时，长 `IN`、解析开销与内存拼装都可能超过一条写好 JOIN 的成本。正确做法是先数 SQL 条数、再看 `EXPLAIN` 与返回行数，按数据量在 `Preload`、JOIN、分两阶段手动查询之间选，而不是把 `Preload` 当银弹。

**误解二：迁移 DDL 和版本记录放同一个事务就万事大吉，所有数据库都一样。** 错。MySQL 的 DDL 会隐式提交，事务包不住 `ALTER TABLE`，仍可能出现「表已改、版本记录回滚」的半迁移状态；SQLite 又不支持并发 DDL。第 3 节那个最小迁移器在 Postgres 与 SQLite 单实例下成立，换到 MySQL 就要补 advisory lock、版本表与失败检测。忽略这个平台差异，是发布流水线里「重启后重复执行 DDL 直接报错」的常见根因。

### 与后续阶段的呼应

- 本讲的 SQL 可观测性在 /microservice/tracing 会变成一次请求里的 SQL span 数量与耗时，在 /microservice/metrics 变成慢查询与 `RowsAffected=0` 的告警规则；/microservice/resilience 的重试预算决定下游慢时要不要继续放大查询。
- N+1 与连接池的关系直接沿用 /server/database-sql 的 `WaitCount` 推导；跨库事务则交给 /distributed/distributed-tx。
- L10 的 /projects/data-sql-tool 要把 `EXPLAIN` 的执行计划做成通用查询工具，本讲「先数条数、再看计划」正是它的诊断顺序；/projects/micro-ecommerce 的商品与订单查询会同时用到预加载、显式列字段和版本化迁移。

选型时别问「哪个最好」，问「这个模块最怕什么」。后台管理页大量简单 CRUD、字段常变，GORM 的关联与钩子能省很多代码；报表与数据分析 SQL 复杂、要精细调优，sqlx 或 sqlc 让 SQL 完全可见更安心；核心交易链路要求编译期类型检查与零反射开销，sqlc 很有吸引力。同一个项目里按模块混用也完全正常，只要底层共享同一个 `sql.DB` 连接池。

<Compare title="关联加载：循环查还是一次取" :panels='[{"label":"反模式 · 循环里查关联","code":"db.Find(users)\nfor i := range users {\ndb.Model(users[i]).Association(orders).Find(users[i].Orders)\n}","note":"N 个用户触发 1 + N 条 SQL，每条都要借连接；列表一大就把池打满"},{"label":"正解 · 预加载","code":"db.Preload(orders).Find(users)","note":"固定 2 条 SQL：先查主表，再用一条 IN 查询取回全部关联并内存拼装"}]' />

<FlowTimeline title="一次迁移上线流程" :steps='[{"title":"写迁移文件","desc":"带版本号与 up/down 脚本，进代码评审","tone":"blue"},{"title":"预发验证","desc":"在副本库跑一遍 up 与 down","tone":"purple"},{"title":"发布前执行","desc":"由发布流水线单实例执行，避免并发 DDL","tone":"orange"},{"title":"记录版本","desc":"DDL 与版本写入同一事务","tone":"green"},{"title":"应用启动","desc":"校验所需版本已就位，失败则拒绝启动","tone":"gray"}]' direction="horizontal" />

还有几个 ORM 专属反模式值得点名：**`SELECT *`** 在表加列后会悄悄改变结构体映射并多传数据，应显式列字段；**在循环里调用 `First`/`Get`** 是 N+1 的另一种写法，改成一条 `IN` 查询；**忽略 `RowsAffected`** 会让「更新了 0 行」被当成成功；**把 GORM 的 `*gorm.DB` 直接当全局裸用而不带 `WithContext`**，请求取消就传不下去。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l6-server/lesson-066-orm`](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-066-orm)。clone 仓库后进入 `examples/l6-server`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="把 N+1 列表接口改成预加载" task="现有 listUsers 先 Find 出全部用户，再循环查每个用户的订单。请写出改造后的查询函数，要求 SQL 条数不随用户数增长，并透传请求 context。" :hints='["用 Preload 一次取回 has-many 关联","把 r.Context() 通过 WithContext 传给查询","检查链式调用返回的 Error，而不是忽略它"]' solution="func listUsers(ctx context.Context, db *gorm.DB) ([]User, error) {\nvar users []User\nif err := db.WithContext(ctx).Preload(orders).Find(users).Error; err != nil {\nreturn nil, err\n}\nreturn users, nil\n}" explain="预加载把关联查询从 N 条压成 1 条 IN 查询；若关联数据量大，也可用 Joins 或分两阶段手动 IN 查询进一步控制内存。" />

## 随堂检测

<Quiz no="066-1" q="关于 sqlc，下列描述正确的是？" :options='["它在运行时用反射把结构体映射成表","它从开发者写的 SQL 生成类型安全的 Go 代码","它是 database/sql 的替代品，自带连接池","它内置了 AutoMigrate 与关联预加载"]' answer="1" explain="sqlc 是编译期代码生成器：读 .sql 文件产出强类型函数，SQL 仍由开发者写，不接管连接池，也不管迁移。" />

<Predict no="066-2" q="一个列表接口查出 200 个用户，每个用户在循环里查一次订单，并发 50 个请求，最可能出现的现象是？" :options='["SQL 总条数不变，只是稍慢","连接池等待队列暴涨，延迟飙升","GORM 自动合并成一条 IN 查询","只有第一个请求受影响"]' answer="1" output="单请求发出 201 条 SQL，50 并发下连接池被瞬间借空，WaitCount 暴涨" explain="1+N 条 SQL 会按并发倍数放大；连接池的 MaxOpenConns 成为硬瓶颈，后续请求全部阻塞在等待队列。" />

## 一页纸速记

<KnowledgeCard :points='["GORM 全包装、sqlx 只自动化 Scan、sqlc 编译期生成，抽象层次依次降低","Preload 把 1+N 条 SQL 压成固定 2 条，是 ORM 必会的优化","生产用版本化迁移：版本表加事务，DDL 与记录同进同退","所有查询用 WithContext 透传请求 context，写路径检查 RowsAffected"]' :pitfalls='["循环里逐条查关联，N+1 打满连接池","把 AutoMigrate 用在生产环境","SELECT * 后表加列，映射悄悄变化","忽略链式调用的 Error 或 RowsAffected","多实例并发执行迁移 DDL"]' summary="选 ORM 的本质是选择「让渡多少 SQL 控制权」；无论选谁，都要能看见它生成的 SQL。" />

## 小结与下一讲

- 三种工具是同一条光谱上的不同刻度，按模块风险与 SQL 复杂度混用完全合理。
- N+1 是 ORM 头号性能杀手，用 `Preload` 或手写 `IN` 查询把 SQL 条数固定下来。
- 迁移要版本化、事务化、可重复执行；`AutoMigrate` 只留在开发环境。

数据层已经能读写了，但每次都回源数据库，扛不住读多写少的流量。下一讲给数据层加缓存，并正面处理缓存三大经典问题。

[下一讲：Redis 缓存：穿透 / 击穿 / 雪崩与一致性](/server/redis)

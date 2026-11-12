---
title: 第 66 讲 · GORM / sqlx / sqlc 与迁移
description: 对比 GORM、sqlx、sqlc 的取舍，讲清模型与关联、预加载与 N+1、版本化迁移和 SQL 可观测性，在 ORM 与原生 SQL 之间做理性选择
---

<StageHeader stage="L6 · 服务端与数据" level="★★★☆☆" time="45 分钟" goal="在 ORM 与原生 SQL 之间做理性选择" prereq="第 65 讲 · database/sql" />

# 第 66 讲 · GORM / sqlx / sqlc 与迁移

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

## 4. 工程实践

选型时别问「哪个最好」，问「这个模块最怕什么」。后台管理页大量简单 CRUD、字段常变，GORM 的关联与钩子能省很多代码；报表与数据分析 SQL 复杂、要精细调优，sqlx 或 sqlc 让 SQL 完全可见更安心；核心交易链路要求编译期类型检查与零反射开销，sqlc 很有吸引力。同一个项目里按模块混用也完全正常，只要底层共享同一个 `sql.DB` 连接池。

<Compare title="关联加载：循环查还是一次取" :panels='[{"label":"反模式 · 循环里查关联","code":"db.Find(users)\nfor i := range users {\ndb.Model(users[i]).Association(orders).Find(users[i].Orders)\n}","note":"N 个用户触发 1 + N 条 SQL，每条都要借连接；列表一大就把池打满"},{"label":"正解 · 预加载","code":"db.Preload(orders).Find(users)","note":"固定 2 条 SQL：先查主表，再用一条 IN 查询取回全部关联并内存拼装"}]' />

<FlowTimeline title="一次迁移上线流程" :steps='[{"title":"写迁移文件","desc":"带版本号与 up/down 脚本，进代码评审","tone":"blue"},{"title":"预发验证","desc":"在副本库跑一遍 up 与 down","tone":"purple"},{"title":"发布前执行","desc":"由发布流水线单实例执行，避免并发 DDL","tone":"orange"},{"title":"记录版本","desc":"DDL 与版本写入同一事务","tone":"green"},{"title":"应用启动","desc":"校验所需版本已就位，失败则拒绝启动","tone":"gray"}]' direction="horizontal" />

还有几个 ORM 专属反模式值得点名：**`SELECT *`** 在表加列后会悄悄改变结构体映射并多传数据，应显式列字段；**在循环里调用 `First`/`Get`** 是 N+1 的另一种写法，改成一条 `IN` 查询；**忽略 `RowsAffected`** 会让「更新了 0 行」被当成成功；**把 GORM 的 `*gorm.DB` 直接当全局裸用而不带 `WithContext`**，请求取消就传不下去。

## 动手练一练

<Exercise title="把 N+1 列表接口改成预加载" task="现有 listUsers 先 Find 出全部用户，再循环查每个用户的订单。请写出改造后的查询函数，要求 SQL 条数不随用户数增长，并透传请求 context。" :hints='["用 Preload 一次取回 has-many 关联","把 r.Context() 通过 WithContext 传给查询","检查链式调用返回的 Error，而不是忽略它"]' solution="func listUsers(ctx context.Context, db *gorm.DB) ([]User, error) {\nvar users []User\nif err := db.WithContext(ctx).Preload(orders).Find(users).Error; err != nil {\nreturn nil, err\n}\nreturn users, nil\n}" explain="预加载把关联查询从 N 条压成 1 条 IN 查询；若关联数据量大，也可用 Joins 或分两阶段手动 IN 查询进一步控制内存。" />

## 随堂检测

<Quiz no="66-1" q="关于 sqlc，下列描述正确的是？" :options='["它在运行时用反射把结构体映射成表","它从开发者写的 SQL 生成类型安全的 Go 代码","它是 database/sql 的替代品，自带连接池","它内置了 AutoMigrate 与关联预加载"]' answer="1" explain="sqlc 是编译期代码生成器：读 .sql 文件产出强类型函数，SQL 仍由开发者写，不接管连接池，也不管迁移。" />

<Predict no="66-2" q="一个列表接口查出 200 个用户，每个用户在循环里查一次订单，并发 50 个请求，最可能出现的现象是？" :options='["SQL 总条数不变，只是稍慢","连接池等待队列暴涨，延迟飙升","GORM 自动合并成一条 IN 查询","只有第一个请求受影响"]' answer="1" output="单请求发出 201 条 SQL，50 并发下连接池被瞬间借空，WaitCount 暴涨" explain="1+N 条 SQL 会按并发倍数放大；连接池的 MaxOpenConns 成为硬瓶颈，后续请求全部阻塞在等待队列。" />

## 一页纸速记

<KnowledgeCard :points='["GORM 全包装、sqlx 只自动化 Scan、sqlc 编译期生成，抽象层次依次降低","Preload 把 1+N 条 SQL 压成固定 2 条，是 ORM 必会的优化","生产用版本化迁移：版本表加事务，DDL 与记录同进同退","所有查询用 WithContext 透传请求 context，写路径检查 RowsAffected"]' :pitfalls='["循环里逐条查关联，N+1 打满连接池","把 AutoMigrate 用在生产环境","SELECT * 后表加列，映射悄悄变化","忽略链式调用的 Error 或 RowsAffected","多实例并发执行迁移 DDL"]' summary="选 ORM 的本质是选择「让渡多少 SQL 控制权」；无论选谁，都要能看见它生成的 SQL。" />

## 小结与下一讲

- 三种工具是同一条光谱上的不同刻度，按模块风险与 SQL 复杂度混用完全合理。
- N+1 是 ORM 头号性能杀手，用 `Preload` 或手写 `IN` 查询把 SQL 条数固定下来。
- 迁移要版本化、事务化、可重复执行；`AutoMigrate` 只留在开发环境。

数据层已经能读写了，但每次都回源数据库，扛不住读多写少的流量。下一讲给数据层加缓存，并正面处理缓存三大经典问题。

[下一讲：Redis 缓存：穿透 / 击穿 / 雪崩与一致性](/server/redis)

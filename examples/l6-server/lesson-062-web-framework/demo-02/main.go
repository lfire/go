package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type CreateUserReq struct {
	Name  string `json:"name" binding:"required,min=2,max=32"`
	Email string `json:"email" binding:"required,email"`
	Age   int    `json:"age" binding:"gte=0,lte=150"`
}

// costMiddleware 展示 c.Next() 的前后置语义，和标准库的洋葱完全一致。
func costMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next() // 先执行后续中间件与业务 handler
		log.Printf("path=%s status=%d cost=%v",
			c.FullPath(), c.Writer.Status(), time.Since(start))
	}
}

func main() {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), costMiddleware())

	v1 := r.Group("/v1")
	{
		users := v1.Group("/users")
		users.GET("", listUsers)
		users.GET("/:id", getUser)
		users.POST("", createUser)
	}

	log.Fatal(r.Run(":8080"))
}

func listUsers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": []string{"gopher"}})
}

func getUser(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
}

func createUser(c *gin.Context) {
	var req CreateUserReq
	// ShouldBindJSON 只绑定、不写响应；BindJSON 会自动写 400 并中断。
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": req})
}

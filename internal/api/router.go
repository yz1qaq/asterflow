package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type ReadinessChecker interface {
	Ping(ctx context.Context) error
}

func NewRouter(checker ReadinessChecker) http.Handler {
	router := gin.New()
	//路径存在但是方法错误,返回405
	router.HandleMethodNotAllowed = true
	//关闭尾斜杠重定向,返回404
	router.RedirectTrailingSlash = false

	router.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	router.GET("/readyz", func(c *gin.Context) {
		if checker == nil {
			c.String(http.StatusServiceUnavailable, "not ready")
			return
		}

		ctx, cancel := context.WithTimeout(
			c.Request.Context(),
			2*time.Second,
		)
		defer cancel()

		if err := checker.Ping(ctx); err != nil || ctx.Err() != nil {
			c.String(http.StatusServiceUnavailable, "not ready")
			return
		}

		c.String(http.StatusOK, "ok")
	})
	return router
}

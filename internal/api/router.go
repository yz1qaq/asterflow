package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func NewRouter() http.Handler {
	router := gin.New()
	//路径存在但是方法错误,返回405
	router.HandleMethodNotAllowed = true
	//关闭尾斜杠重定向,返回404
	router.RedirectTrailingSlash = false

	router.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return router
}

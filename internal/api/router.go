package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/api/middleware"
	v1 "github.com/yz1qaq/asterflow/internal/api/v1"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
)

type ReadinessChecker interface {
	Ping(ctx context.Context) error
}

func NewRouter(
	checker ReadinessChecker,
	iamService *iam.Service,
) http.Handler {
	router := gin.New()
	router.Use(gin.Recovery())
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
	iamHandler := v1.NewIAMHandler(iamService)

	v1Routes := router.Group("/api/v1")
	v1Routes.POST("/auth/login", iamHandler.Login)

	protected := v1Routes.Group("")
	protected.Use(middleware.Auth(iamService))

	protected.GET("/me", iamHandler.Me)
	protected.GET("/workspaces", iamHandler.ListWorkspaces)
	protected.POST("/workspaces", iamHandler.CreateWorkspace)
	protected.DELETE("/workspaces/:workspace_id/members/:user_id",
		iamHandler.DeleteWorkspaceMember)

	protected.GET(
		"/workspaces/:workspace_id/members",
		iamHandler.ListWorkspaceMembers,
	)

	protected.PUT(
		"/workspaces/:workspace_id/members/:user_id",
		iamHandler.UpsertWorkspaceMember,
	)
	protected.GET(
		"/context",
		middleware.Workspace(iamService),
		iamHandler.Context,
	)

	adminRoutes := v1Routes.Group("")
	adminRoutes.Use(
		middleware.Auth(iamService),
		middleware.RequireGlobalAdmin(),
	)
	adminRoutes.POST("/auth/register", iamHandler.CreateUser)
	adminRoutes.GET("/users", iamHandler.ListUsers)

	return router
}

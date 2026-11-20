package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/api/middleware"
	v1 "github.com/yz1qaq/asterflow/internal/api/v1"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
)

type ReadinessChecker interface {
	Ping(ctx context.Context) error
}

func NewRouter(
	checker ReadinessChecker,
	iamService *iam.Service,
	modelService *modelconfig.Service,
	promptService *prompt.Service,
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

	modelHandler := v1.NewModelConfigHandler(modelService)

	workspaceRoutes := protected.Group("")
	workspaceRoutes.Use(middleware.Workspace(iamService))

	workspaceRoutes.POST(
		"/model-accounts",
		modelHandler.CreateProviderAccount,
	)
	workspaceRoutes.GET(
		"/model-accounts",
		modelHandler.ListProviderAccounts,
	)
	workspaceRoutes.PUT(
		"/model-accounts/:account_id/api-key",
		modelHandler.RotateProviderAPIKey,
	)

	workspaceRoutes.POST(
		"/model-deployments",
		modelHandler.CreateDeployment,
	)
	workspaceRoutes.GET(
		"/model-deployments",
		modelHandler.ListDeployments,
	)
	workspaceRoutes.PUT(
		"/model-deployments/:deployment_id/pricing",
		modelHandler.UpdateDeploymentPricing,
	)

	workspaceRoutes.POST(
		"/model-profiles",
		modelHandler.CreateProfile,
	)
	workspaceRoutes.GET(
		"/model-profiles",
		modelHandler.ListProfiles,
	)
	workspaceRoutes.POST(
		"/model-profiles/:profile_id/versions",
		modelHandler.CreateProfileVersion,
	)
	workspaceRoutes.GET(
		"/model-profiles/:profile_id/versions",
		modelHandler.ListProfileVersions,
	)

	workspaceRoutes.GET(
		"/model-profile-versions",
		modelHandler.ListWorkspaceProfileVersions,
	)
	workspaceRoutes.GET(
		"/model-bindings",
		modelHandler.GetProjectBinding,
	)
	workspaceRoutes.PUT(
		"/model-bindings",
		modelHandler.BindProject,
	)

	promptHandler := v1.NewPromptHandler(promptService)

	workspaceRoutes.GET("/prompts", promptHandler.ListPrompts)
	workspaceRoutes.POST("/prompts", promptHandler.CreatePrompt)
	workspaceRoutes.GET("/prompts/:prompt_id/versions", promptHandler.ListVersions)
	workspaceRoutes.POST("/prompts/:prompt_id/versions", promptHandler.CreateVersion)
	workspaceRoutes.POST("/prompts/:prompt_id/render", promptHandler.Render)
	workspaceRoutes.POST("/prompts/:prompt_id/promote", promptHandler.Promote)
	workspaceRoutes.POST("/prompts/:prompt_id/rollback", promptHandler.Rollback)

	workspaceRoutes.GET("/prompts/:prompt_id/envs", promptHandler.ListEnvs)
	workspaceRoutes.GET("/prompts/:prompt_id/diff", promptHandler.Diff)

	workspaceRoutes.GET("/prompts/:prompt_id/experiments/active", promptHandler.GetActiveExperiment)
	workspaceRoutes.POST("/prompts/:prompt_id/experiments", promptHandler.StartExperiment)
	workspaceRoutes.POST("/prompts/:prompt_id/rollouts", promptHandler.StartRollout)
	workspaceRoutes.PUT("/prompts/:prompt_id/rollouts/traffic", promptHandler.UpdateRolloutTraffic)
	workspaceRoutes.POST("/prompts/:prompt_id/rollouts/complete", promptHandler.CompleteRollout)
	workspaceRoutes.POST("/prompts/:prompt_id/rollouts/rollback", promptHandler.RollbackRollout)
	workspaceRoutes.GET("/prompts/:prompt_id/experiments/:experiment_id/events", promptHandler.ListRolloutEvents)
	workspaceRoutes.GET("/prompts/:prompt_id/env-events", promptHandler.ListEnvEvents)

	adminRoutes := v1Routes.Group("")
	adminRoutes.Use(
		middleware.Auth(iamService),
		middleware.RequireGlobalAdmin(),
	)
	adminRoutes.POST("/auth/register", iamHandler.CreateUser)
	adminRoutes.GET("/users", iamHandler.ListUsers)

	return router
}

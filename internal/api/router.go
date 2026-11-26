package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yz1qaq/asterflow/internal/api/middleware"
	v1 "github.com/yz1qaq/asterflow/internal/api/v1"
	"github.com/yz1qaq/asterflow/internal/platform/agent"
	"github.com/yz1qaq/asterflow/internal/platform/conversation"
	"github.com/yz1qaq/asterflow/internal/platform/iam"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
	"github.com/yz1qaq/asterflow/internal/platform/prompt"
	"github.com/yz1qaq/asterflow/internal/platform/tool"
	"github.com/yz1qaq/asterflow/internal/runtime/tooling"
)

type ReadinessChecker interface {
	Ping(ctx context.Context) error
}

type Services struct {
	Agent        *agent.Service
	Conversation *conversation.Service
	Tool         *tool.Service
	ToolRegistry *tooling.Registry
}

func NewRouter(
	checker ReadinessChecker,
	iamService *iam.Service,
	modelService *modelconfig.Service,
	promptService *prompt.Service,
	extra ...Services,
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

	workspaceRoutes.DELETE("/model-accounts/:account_id", modelHandler.DeleteProviderAccount)
	workspaceRoutes.POST("/model-accounts/:account_id/test", modelHandler.TestProviderAccount)

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
	workspaceRoutes.DELETE("/prompts/:prompt_id/versions/:version_id", promptHandler.DeleteDraft)
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

	var services Services
	if len(extra) > 0 {
		services = extra[0]
	}
	if services.Tool != nil {
		toolHandler := v1.NewToolHandler(services.Tool, services.ToolRegistry)
		workspaceRoutes.GET("/tools", toolHandler.ListTools)
		workspaceRoutes.GET("/tools/:tool_id/versions", toolHandler.ListToolVersions)
		workspaceRoutes.POST("/tools", toolHandler.CreateTool)
		workspaceRoutes.POST("/tools/:tool_id/versions", toolHandler.CreateToolVersion)
		if services.ToolRegistry != nil {
			workspaceRoutes.POST("/tools/:tool_id/test", toolHandler.TestTool)
		}
		workspaceRoutes.POST("/tools/:tool_id/versions/:version_id/publish", toolHandler.PublishToolVersion)
	}
	if services.Agent != nil {
		agentHandler := v1.NewAgentHandler(services.Agent)
		workspaceRoutes.GET("/agents", agentHandler.ListAgents)
		workspaceRoutes.POST("/agents", agentHandler.CreateAgent)
		workspaceRoutes.GET("/agents/:agent_id", agentHandler.GetAgent)
		workspaceRoutes.DELETE("/agents/:agent_id", agentHandler.Delete)
		workspaceRoutes.GET("/agents/:agent_id/versions", agentHandler.ListVersions)
		workspaceRoutes.POST("/agents/:agent_id/versions", agentHandler.CreateVersion)
		workspaceRoutes.GET("/agents/:agent_id/envs", agentHandler.ListEnvs)
		workspaceRoutes.POST("/agents/:agent_id/promote", agentHandler.Promote)

	}
	if services.Conversation != nil {
		conversationHandler := v1.NewConversationHandler(services.Conversation)
		workspaceRoutes.GET("/conversations", conversationHandler.List)
		workspaceRoutes.POST("/conversations", conversationHandler.Create)
		workspaceRoutes.GET("/conversations/:conversation_id", conversationHandler.Get)
		workspaceRoutes.DELETE("/conversations/:conversation_id", conversationHandler.Delete)

		conversationChatHandler := v1.NewConversationChatHandler(services.Conversation)
		workspaceRoutes.POST("/conversations/:conversation_id/chat", conversationChatHandler.Chat)
	}

	adminRoutes := v1Routes.Group("")
	adminRoutes.Use(
		middleware.Auth(iamService),
		middleware.RequireGlobalAdmin(),
	)
	adminRoutes.POST("/auth/register", iamHandler.CreateUser)
	adminRoutes.GET("/users", iamHandler.ListUsers)

	return router
}

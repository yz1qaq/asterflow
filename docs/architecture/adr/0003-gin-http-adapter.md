# ADR-0003：Gin HTTP接入适配

日期2026-11-15，状态：已采纳，负责人明确确认后端由chi改为Gin，前端保留React。

## 决策

后端HTTP框架固定Gin v1.12.0；项目Go 1.26.6满足该版go.mod的Go 1.25要求。前端React/TypeScript和全部既定产品能力保持。
NewRouter()继续返回http.Handler；http.Server启动/关闭逻辑、Service/Engine接口不依赖Gin。gin.Context负责API参数/响应和中间件，调用业务时使用c.Request.Context()，不得将复用的Gin Context传到长期后台任务。

## 当前行为适配

使用gin.New()按阶段显式装配中间件，不借gin.Default()隐式改变既定链。当前healthz不提前扩展功能；Recovery/日志/身份/空间等在对应任务明确实现与顺序。
Gin默认HandleMethodNotAllowed=false，错误方法返回404；显式设为true，维持POST /healthz=405。
Gin默认RedirectTrailingSlash=true，会重定向尾斜杠路径；显式设为false，维持GET /healthz/=404。GET /healthz仍200、text/plain、ok；未知与子路径404。
复用原有框架无关router_test.go，不修改断言迁就框架默认值。具体中间件、SSE/WS/Flusher与错误响应逐阶段对齐完整契约，不仅检查能编译。

## 范围与验证

改的是HTTP适配，不削减模块，不改主要业务链、版本/租约/权限/消息语义。上游快照清单仍保存原chi实现归属，目标映射单独记录。
负责人已保存正式Gin依赖和生产router.go，配置/路由/进程生命周期与入口均已本地验收；本分支尚未提交/推送，待远程CI和按SHA正式Review。

官方依据：[v1.12.0发布](https://github.com/gin-gonic/gin/releases/tag/v1.12.0)、[版本API](https://pkg.go.dev/github.com/gin-gonic/gin@v1.12.0)、[版本go.mod](https://github.com/gin-gonic/gin/blob/v1.12.0/go.mod)。

# G07 实时工具事件

固定 Final 在 ADK 的工具执行中间件发 tool_call 和 tool_result。AsterFlow 延续调用前后发事件，现有 SSE 序号负责排序，call_id 负责把结果连回具体调用。本批为学员手写候选。执行失败以简明结果反馈给模型，取消和审批中断仍交给框架处理。参数使用字符串承载，错误参数也可被准确传输。

上一批 ADK 固定工具调用已保存，正式 Engine race 通过。当前工具事件候选通过 Engine race，React 工具区已接入并通过 web-check。隔离桌面 Chrome 使用本地模型替身和真实 Crossborder PostgreSQL 验证成功订单和缺少参数两条路径，SSE 顺序与调用 ID 正确，可展开查看参数和结果，本轮错误能回喂模型。

## `internal/runtime/engine/events.go`

```diff
--- a/internal/runtime/engine/events.go
+++ b/internal/runtime/engine/events.go
@@ -13,6 +13,8 @@
    EventRunStarted     EventType = "run_started"
    EventPromptResolved EventType = "prompt_resolved"
    EventModelDelta     EventType = "model_delta"
+	EventToolCall       EventType = "tool_call"
+	EventToolResult     EventType = "tool_result"

    EventRunCompleted EventType = "run_completed"
    EventRunFailed    EventType = "run_failed"
@@ -48,6 +50,21 @@
    Content string `json:"content"`
 }

+type ToolCallEvent struct {
+	CallID    string `json:"call_id"`
+	Name      string `json:"name"`
+	Arguments string `json:"arguments"`
+}
+
+type ToolResultEvent struct {
+	CallID    string `json:"call_id"`
+	Name      string `json:"name"`
+	Status    string `json:"status"`
+	Output    string `json:"output,omitempty"`
+	Error     string `json:"error,omitempty"`
+	LatencyMs int64  `json:"latency_ms"`
+}
+
 type RunCompleted struct {
    AssistantMessageID string    `json:"assistant_message_id"`
    Usage              llm.Usage `json:"usage"`
```

## `internal/runtime/engine/adk.go`

```diff
--- a/internal/runtime/engine/adk.go
+++ b/internal/runtime/engine/adk.go
@@ -129,7 +129,7 @@

 func runADK(
    ctx context.Context, chatModel model.BaseChatModel, system string, messages []*schema.Message,
-	tools []einotool.BaseTool, onDelta func(string) error,
+	tools []einotool.BaseTool, toolEvents compose.ToolMiddleware, onDelta func(string) error,
 ) (*RunResult, error) {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()
@@ -147,6 +147,7 @@
        config.Handlers = []adk.ChatModelAgentMiddleware{patch}
        config.ToolsConfig = adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: tools, ExecuteSequentially: true,
+			ToolCallMiddlewares: []compose.ToolMiddleware{toolEvents},
            UnknownToolsHandler: func(_ context.Context, name, _ string) (string, error) {
                return fmt.Sprintf("error: tool %q is not available", name), nil
            },
```

## `internal/runtime/engine/executor.go`

```diff
--- a/internal/runtime/engine/executor.go
+++ b/internal/runtime/engine/executor.go
@@ -191,7 +191,7 @@
            Seed:                  runtime.GenerationConfig.Seed,
        },
    }
-	result, err := runADK(ctx, chatModel, system, messages, tools, func(text string) error {
+	result, err := runADK(ctx, chatModel, system, messages, tools, toolEventMiddleware(send), func(text string) error {
        return send(EventModelDelta, ModelDelta{Content: text})
    })
    if err != nil {
```

## 新建 `internal/runtime/engine/tool_events.go`

```go
package engine

import (
	"context"
	"time"

	"github.com/cloudwego/eino/compose"
)

func toolEventMiddleware(send func(EventType, any) error) compose.ToolMiddleware {
	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				// 让客户端在工具实际执行前看到调用参数和调用 ID。
				if err := send(EventToolCall, ToolCallEvent{
					CallID: input.CallID, Name: input.Name, Arguments: input.Arguments,
				}); err != nil {
					return nil, err
				}

				start := time.Now()
				output, runErr := next(ctx, input)
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if _, interrupted := compose.IsInterruptRerunError(runErr); interrupted {
					return nil, runErr
				}

				result := ToolResultEvent{
					CallID: input.CallID, Name: input.Name,
					Status: "success", LatencyMs: time.Since(start).Milliseconds(),
				}
				if runErr != nil || output == nil {
					// 给网页和模型可读的失败结果，避免把底层请求错误带出去。
					result.Status = "error"
					result.Error = "工具执行失败"
					output = &compose.ToolOutput{Result: "error: tool execution failed"}
				} else {
					result.Output = output.Result
				}

				if err := send(EventToolResult, result); err != nil {
					return nil, err
				}
				return output, nil
			}
		},
	}
}
```


## 后续

学员已保存，助手合入正式工具事件回归，Engine race 通过。下一批[工具消息入库与跨轮回放](G07-persist-tool-history.md)。

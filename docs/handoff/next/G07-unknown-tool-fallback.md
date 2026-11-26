# G07 未知工具回退的事件与历史

固定 Final 的 UnknownToolsHandler 会返回工具不可用信息，模型可以据此继续生成。AsterFlow 使用相同回退方式，并记录未知调用的失败轨迹。已验证 Eino v0.9.15 的未知工具任务绕过工具中间件和 ADK 工具结果事件，模型仍收到回退文本。

在 UnknownToolsHandler 使用 compose.GetToolCallID 获取原调用 ID。通过 Executor 的 sendToolEvent 发送 tool_call 和失败的 tool_result，同时把结果写入 RunResult.ToolResults。使用 adk.SendEvent 补一条 ADK tool 消息，使 runADK 原有的输出循环收集完整轨迹。返回同样的文本，ToolsNode 正常把它传给下一次模型请求。未知工具没有已绑定版本记录。

已用失败的 Engine 回归复现旧实现。候选连续五次通过 Engine race，正式实现通过 Engine、会话和 HTTP race。隔离桌面与真实 PostgreSQL 验证两个已绑定的 REST 工具正常调用，未知工具能继续回答，SSE 失败与刷新历史保留同一个模型调用 ID。已按负责人授权保存 Go 实现，并将回归并入现有 adk_test.go。已用正式源码重建、通过隔离桌面复验。

## `internal/runtime/engine/executor.go`

```diff
--- a/internal/runtime/engine/executor.go
+++ b/internal/runtime/engine/executor.go
@@ -201,7 +201,7 @@
        }
        return send(kind, data)
    }
-	result, err := runADK(ctx, chatModel, system, messages, tools, toolEventMiddleware(sendToolEvent), func(text string) error {
+	result, err := runADK(ctx, chatModel, system, messages, tools, sendToolEvent, func(text string) error {
        return send(EventModelDelta, ModelDelta{Content: text})
    })
    if err != nil {
```

## `internal/runtime/engine/adk.go`

```diff
--- a/internal/runtime/engine/adk.go
+++ b/internal/runtime/engine/adk.go
@@ -129,7 +129,7 @@

 func runADK(
    ctx context.Context, chatModel model.BaseChatModel, system string, messages []*schema.Message,
-	tools []einotool.BaseTool, toolEvents compose.ToolMiddleware, onDelta func(string) error,
+	tools []einotool.BaseTool, sendToolEvent func(EventType, any) error, onDelta func(string) error,
 ) (*RunResult, error) {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()
@@ -147,9 +147,29 @@
        config.Handlers = []adk.ChatModelAgentMiddleware{patch}
        config.ToolsConfig = adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: tools, ExecuteSequentially: true,
-			ToolCallMiddlewares: []compose.ToolMiddleware{toolEvents},
-			UnknownToolsHandler: func(_ context.Context, name, _ string) (string, error) {
-				return fmt.Sprintf("error: tool %q is not available", name), nil
+			ToolCallMiddlewares: []compose.ToolMiddleware{toolEventMiddleware(sendToolEvent)},
+			UnknownToolsHandler: func(toolCtx context.Context, name, args string) (string, error) {
+				// 未知工具走 Eino 的回退路径，从上下文取原来的调用 ID。
+				callID := compose.GetToolCallID(toolCtx)
+				if err := sendToolEvent(EventToolCall, ToolCallEvent{
+					CallID: callID, Name: name, Arguments: args,
+				}); err != nil {
+					return "", err
+				}
+				// 用同一个 ID 标记失败，供实时界面和历史记录配对。
+				if err := sendToolEvent(EventToolResult, ToolResultEvent{
+					CallID: callID, Name: name, Status: "error", Error: "工具不在本轮可用列表中",
+				}); err != nil {
+					return "", err
+				}
+				output := fmt.Sprintf("error: tool %q is not available", name)
+				// 回退函数绕过 ADK 的工具事件包装，显式发送消息供既有入库逻辑收集。
+				message := schema.ToolMessage(output, callID, schema.WithToolName(name))
+				if err := adk.SendEvent(toolCtx, adk.EventFromMessage(message, nil, schema.Tool, name)); err != nil {
+					return "", err
+				}
+				// 返回同一条结果，由 Eino 放进下一次模型请求。
+				return output, nil
            },
        }}
    }
```

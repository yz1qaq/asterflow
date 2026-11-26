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
				// 先通知客户端，再执行工具。
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

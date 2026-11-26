# G07 把快照工具版本传到 Engine

固定 Final 在每轮对话按会话的 AgentVersionID 读取 Agent 快照，再将其中 ToolVersionIDs 交给 buildTools。隔离参考实例通过虚构 Agent 和 get_order v1 核对版本 UUID 已写入快照。

AsterFlow 在 conversation.RunTurn 读取快照，复用 GetOwnedVersion 的工作空间和 Agent 归属检查。现有显式用户提示词版本选择也使用这次读取的快照。RunInput.ToolVersionIDs 为服务端内部字段，传递的是具体版本 UUID，来源是会话所指向的不可变 Agent 版本。

本批为学员手写候选。与上一批 Eino 适配器一起用 Go overlay 跑通隔离 PostgreSQL、会话与聊天 HTTP、Agent HTTP、Engine 和 ToolsNode 的针对性 race 回归。既有会话绑定 Agent v1 时，即使 dev 指向 Agent v2，传到 Engine 的仍是 v1 快照里的工具版本 UUID。学员已保存修正。会话测试已保存，正式工作区会话与 HTTP 隔离 PostgreSQL race 通过。

## `internal/runtime/engine/executor.go`

```diff
--- a/internal/runtime/engine/executor.go
+++ b/internal/runtime/engine/executor.go
@@ -32,6 +32,7 @@

 type RunInput struct {
    AgentVersionID      string            `json:"-"`
+	ToolVersionIDs      []string          `json:"-"`
    TurnRevision        int64             `json:"-"`
    WorkspaceID         string            `json:"-"`
    ConversationID      string            `json:"-"`
```

## `internal/platform/conversation/chat.go`

```diff
--- a/internal/platform/conversation/chat.go
+++ b/internal/platform/conversation/chat.go
@@ -96,16 +96,29 @@
        )
    }

+	// 从会话固定的 Agent 版本取快照，避免切换环境后改变旧会话的工具。
+	snapshot, err := s.loadAgentSnapshot(turnCtx, conv)
+	if cause := context.Cause(turnCtx); cause != nil {
+		return nil, cause
+	}
+	if err != nil {
+		return nil, err
+	}
+
    var userPromptID string
    if requestedVersion != "" {
-		userPromptID, err = s.boundUserPromptID(turnCtx, conv)
-		if err != nil {
-			return nil, err
-		}
+		if snapshot.UserPromptID == "" {
+			return nil, fmt.Errorf(
+				"%w: agent version has no user prompt template for version selection",
+				ErrInvalidArgument,
+			)
+		}
+		userPromptID = snapshot.UserPromptID
    }

    result, err := s.engine.Run(turnCtx, engine.RunInput{
        AgentVersionID:      conv.AgentVersionID,
+		ToolVersionIDs:      snapshot.ToolVersionIDs,
        TurnRevision:        revision,
        WorkspaceID:         workspaceID,
        ConversationID:      conv.ID,
@@ -185,30 +198,23 @@
    return assistantMessage, nil
 }

-// 会话固定的agent版本读取模版ID
-func (s *Service) boundUserPromptID(
+func (s *Service) loadAgentSnapshot(
    ctx context.Context, conv *domain.Conversation,
-) (string, error) {
+) (domain.AgentSnapshot, error) {
+	var snapshot domain.AgentSnapshot
    if s.agentsChecker == nil {
-		return "", fmt.Errorf("agent resolver is not configured")
-	}
-
+		return snapshot, fmt.Errorf("agent resolver is not configured")
+	}
+
+	// 同时核对工作空间、Agent 和会话保存的 Agent 版本。
    version, err := s.agentsChecker.GetOwnedVersion(
        ctx, conv.WorkspaceID, conv.AgentID, conv.AgentVersionID,
    )
    if err != nil {
-		return "", err
-	}
-
-	var snapshot domain.AgentSnapshot
+		return snapshot, err
+	}
    if err := json.Unmarshal([]byte(version.SnapshotJSON), &snapshot); err != nil {
-		return "", fmt.Errorf("decode agent snapshot: %w", err)
-	}
-	if snapshot.UserPromptID == "" {
-		return "", fmt.Errorf(
-			"%w: agent version has no user prompt template for version selection",
-			ErrInvalidArgument,
-		)
-	}
-	return snapshot.UserPromptID, nil
-}
+		return snapshot, fmt.Errorf("decode agent snapshot: %w", err)
+	}
+	return snapshot, nil
+}
```

## 后续

验证正式工作区后，让 Engine 按这些 VersionID 加载工具配置并构造适配器，然后接入 Eino ADK 及模型网关。聊天工具调用、流式事件与历史消息还待实现。

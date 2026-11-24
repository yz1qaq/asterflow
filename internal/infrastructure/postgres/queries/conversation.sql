-- name: CreateConversation :one
INSERT INTO conversations (id, workspace_id, agent_id, agent_version_id, user_id, status, classification, runtime_config_json)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetConversation :one
SELECT c.*
FROM conversations AS c
WHERE c.id = sqlc.arg(id)::uuid
  AND c.workspace_id = sqlc.arg(workspace_id)::text;

-- name: ListConversations :many
SELECT c.*
FROM conversations AS c
WHERE c.workspace_id = sqlc.arg(workspace_id)::text
  AND (sqlc.narg(user_id)::text IS NULL OR c.user_id = sqlc.narg(user_id)::text)
ORDER BY c.started_at DESC, c.id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: DeleteOwnedConversation :execrows
DELETE FROM conversations
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::text
  AND user_id = sqlc.arg(user_id)::text
  AND (turn_lease_until IS NULL OR turn_lease_until <= clock_timestamp());

-- name: ListConversationMessages :many
SELECT m.*
FROM messages AS m
JOIN conversations AS c ON c.id = m.conversation_id
WHERE m.conversation_id = sqlc.arg(conversation_id)::uuid
  AND c.workspace_id = sqlc.arg(workspace_id)::text
ORDER BY m.created_at, m.id;

-- name: InsertMessage :one
INSERT INTO messages (id, conversation_id, role, content, tool_calls, tool_call_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: InsertToolCall :one
INSERT INTO tool_calls (id, message_id, tool_id, tool_version_id, args, result, latency_ms, error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ClaimConversationTurn :one
UPDATE conversations
SET turn_token = sqlc.arg(turn_token)::uuid,
    turn_lease_until = now() + sqlc.arg(lease_seconds)::int * interval '1 second',
    turn_revision = turn_revision + 1,
    status = 'running',
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::text
  AND (turn_lease_until IS NULL OR turn_lease_until < now())
RETURNING turn_token, turn_revision, turn_lease_until;

-- name: RenewConversationTurn :execrows
UPDATE conversations
SET turn_lease_until = now() + sqlc.arg(lease_seconds)::int * interval '1 second',
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::text
  AND turn_token = sqlc.arg(turn_token)::uuid
  AND turn_lease_until > clock_timestamp();

-- name: FinishConversationTurn :execrows
UPDATE conversations
SET status = sqlc.arg(status)::text,
    turn_token = NULL,
    turn_lease_until = NULL,
    runtime_config_json = runtime_config_json
    || COALESCE(sqlc.narg(user_prompt_snapshot)::jsonb, '{}'::jsonb),
    updated_at = now()

WHERE id = sqlc.arg(id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::text
  AND turn_token = sqlc.arg(turn_token)::uuid
  AND (NOT sqlc.arg(require_active_lease)::boolean OR turn_lease_until > clock_timestamp());

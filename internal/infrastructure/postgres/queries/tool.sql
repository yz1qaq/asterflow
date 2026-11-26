-- name: ListTools :many
SELECT * FROM tools WHERE workspace_id = $1 ORDER BY created_at;

-- name: CreateToolWithVersion :exec
WITH inserted_tool AS (
    INSERT INTO tools (
        id, workspace_id, name, description, source_type,
        sensitive, classification_max, created_by, created_at, updated_at
    )
    VALUES (
        sqlc.arg(tool_id), sqlc.arg(workspace_id), sqlc.arg(name),
        sqlc.arg(description), sqlc.arg(source_type), sqlc.arg(sensitive),
        sqlc.arg(classification_max), sqlc.arg(tool_created_by), now(), now()
    )
    RETURNING id
)
INSERT INTO tool_versions (
    id, tool_id, version, schema_json, endpoint_config, auth_config,
    auth_config_encrypted, retry_policy, status, created_by, created_at
)
SELECT
    sqlc.arg(version_id), id, 1, sqlc.arg(schema_json),
    sqlc.arg(endpoint_config), sqlc.arg(auth_config),
    sqlc.arg(auth_config_encrypted), sqlc.arg(retry_policy),
    sqlc.arg(version_status), sqlc.arg(version_created_by), now()
FROM inserted_tool;

-- name: ListToolVersions :many
SELECT * FROM tool_versions WHERE tool_id = $1 ORDER BY version DESC;


-- name: GetTool :one
SELECT * FROM tools WHERE id = $1 LIMIT 1;



-- name: GetToolCurrentVersion :one
SELECT * FROM tool_versions
WHERE tool_id = $1
ORDER BY version DESC
LIMIT 1;

-- name: LockToolForVersion :one
SELECT id FROM tools
WHERE id = sqlc.arg(tool_id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::text
FOR UPDATE;

-- name: NextToolVersion :one
SELECT COALESCE(MAX(version), 0) + 1 AS next_version
FROM tool_versions
WHERE tool_id = $1;

-- name: InsertToolVersion :one
INSERT INTO tool_versions (
    id, tool_id, version, schema_json, endpoint_config, auth_config,
    auth_config_encrypted, retry_policy, status, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'draft', $9)
RETURNING *;



-- name: CreateToolTestRun :one
INSERT INTO tool_test_runs (
    id, tool_id, tool_version_id, input, output, status, latency_ms, error
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetToolLastSuccessfulTestRunForVersion :one
SELECT *
FROM tool_test_runs
WHERE tool_version_id = $1
  AND status = 'success'
ORDER BY created_at DESC
LIMIT 1;

-- name: GetToolVersion :one
SELECT *
FROM tool_versions
WHERE id = $1;



-- name: PublishToolVersion :execrows
UPDATE tool_versions
SET status = 'published'
WHERE tool_id = sqlc.arg(tool_id)
  AND id = sqlc.arg(version_id)
  AND status = 'draft';


-- name: GetToolLatestPublishedVersionID :one
SELECT id
FROM tool_versions
WHERE tool_id = $1
  AND status = 'published'
ORDER BY version DESC
LIMIT 1;
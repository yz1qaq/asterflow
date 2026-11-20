-- name: CreateAgent :one
INSERT INTO agents (id, workspace_id, name, description, template, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAgent :one
SELECT a.*
FROM agents AS a
WHERE a.id = sqlc.arg(id)::uuid
  AND a.workspace_id = sqlc.arg(workspace_id)::text;

-- name: ListAgents :many
SELECT a.*
FROM agents AS a
WHERE a.workspace_id = sqlc.arg(workspace_id)::text
ORDER BY a.name, a.id;

-- name: LockAgent :one
SELECT a.id
FROM agents AS a
WHERE a.id = sqlc.arg(id)::uuid
  AND a.workspace_id = sqlc.arg(workspace_id)::text
FOR UPDATE;

-- name: CreateAgentVersion :one
INSERT INTO agent_versions (id, agent_id, version, snapshot_json, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: NextAgentVersion :one
SELECT COALESCE(MAX(version), 0) + 1 AS next_version
FROM agent_versions
WHERE agent_id = sqlc.arg(agent_id)::uuid;

-- name: GetAgentVersion :one
SELECT v.*
FROM agent_versions AS v
JOIN agents AS a ON a.id = v.agent_id
WHERE v.id = sqlc.arg(id)::uuid
  AND a.workspace_id = sqlc.arg(workspace_id)::text;

-- name: ListAgentVersions :many
SELECT v.*
FROM agent_versions AS v
JOIN agents AS a ON a.id = v.agent_id
WHERE a.id = sqlc.arg(agent_id)::uuid
  AND a.workspace_id = sqlc.arg(workspace_id)::text
ORDER BY v.version DESC;

-- name: UpsertAgentEnv :one
INSERT INTO agent_envs (agent_id, env, version_id)
VALUES ($1, $2, $3)
ON CONFLICT (agent_id, env) DO UPDATE
SET version_id = EXCLUDED.version_id, updated_at = now()
RETURNING *;

-- name: GetAgentEnv :one
SELECT e.*
FROM agent_envs AS e
JOIN agents AS a ON a.id = e.agent_id
WHERE a.id = sqlc.arg(agent_id)::uuid
  AND a.workspace_id = sqlc.arg(workspace_id)::text
  AND e.env = sqlc.arg(env)::text;



-- name: ListAgentEnvs :many
SELECT e.*
FROM agent_envs AS e
JOIN agents AS a ON a.id = e.agent_id
WHERE a.id = sqlc.arg(agent_id)::uuid
  AND a.workspace_id = sqlc.arg(workspace_id)::text
ORDER BY e.env;
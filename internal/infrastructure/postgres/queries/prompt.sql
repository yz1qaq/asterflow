-- name: CreatePrompt :one
INSERT INTO prompts (id, workspace_id, name, category, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreatePromptVersion :one
INSERT INTO prompt_versions (
    id, prompt_id, version, template, variables_schema,
    model_profile_version_id, generation_config, hash, token_estimate, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: UpsertPromptEnv :one
INSERT INTO prompt_envs (prompt_id, env, version_id)
VALUES ($1, $2, $3)
ON CONFLICT (prompt_id, env) DO UPDATE
SET version_id = EXCLUDED.version_id, updated_at = now()
RETURNING *;



-- name: GetPrompt :one
SELECT *
FROM prompts
WHERE id = sqlc.arg(prompt_id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::text;

-- name: ListPrompts :many
SELECT *
FROM prompts
WHERE workspace_id = $1
ORDER BY created_at, id;

-- name: GetPromptVersion :one
SELECT v.*
FROM prompt_versions AS v
JOIN prompts AS p ON p.id = v.prompt_id
WHERE v.id = sqlc.arg(version_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text;

-- name: ListPromptVersions :many
SELECT v.*
FROM prompt_versions AS v
JOIN prompts AS p ON p.id = v.prompt_id
WHERE p.id = sqlc.arg(prompt_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text
ORDER BY v.version;

-- name: LockPrompt :one
SELECT id
FROM prompts
WHERE id = sqlc.arg(prompt_id)::uuid
  AND workspace_id = sqlc.arg(workspace_id)::text
FOR UPDATE;

-- name: NextPromptVersion :one
SELECT (COALESCE(MAX(version), 0)::bigint + 1)::bigint AS next_version
FROM prompt_versions
WHERE prompt_id = $1;



-- name: GetPromptEnv :one
SELECT e.*
FROM prompt_envs AS e
JOIN prompts AS p ON p.id = e.prompt_id
WHERE p.id = sqlc.arg(prompt_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text
  AND e.env = sqlc.arg(env)::text;



-- name: ListPromptEnvs :many
SELECT e.*
FROM prompt_envs AS e
JOIN prompts AS p ON p.id = e.prompt_id
WHERE p.id = sqlc.arg(prompt_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text
ORDER BY e.env;


-- name: GetPromptVersionByNumber :one
SELECT v.*
FROM prompt_versions AS v
JOIN prompts AS p ON p.id = v.prompt_id
WHERE p.id = sqlc.arg(prompt_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text
  AND v.version = sqlc.arg(version)::int;
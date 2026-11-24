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
  AND NOT v.is_archived
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
  AND v.version = sqlc.arg(version)::int
  AND NOT v.is_archived;

-- name: ArchiveUnusedPromptDraft :execrows
UPDATE prompt_versions AS v
SET is_archived = true
WHERE v.prompt_id = sqlc.arg(prompt_id)::uuid
  AND v.id = sqlc.arg(version_id)::uuid
  AND v.version > 1
  AND NOT v.is_archived
  AND NOT EXISTS (
      SELECT 1 FROM prompt_envs AS e
      WHERE e.prompt_id = v.prompt_id AND e.version_id = v.id
  )
  AND NOT EXISTS (
      SELECT 1 FROM prompt_env_events AS e
      WHERE e.prompt_id = v.prompt_id AND v.id IN (e.from_version_id, e.to_version_id)
  )
  AND NOT EXISTS (
      SELECT 1 FROM prompt_experiments AS e
      WHERE e.prompt_id = v.prompt_id AND (
          v.id IN (e.baseline_version_id, e.candidate_version_id)
          OR EXISTS (
              SELECT 1 FROM jsonb_array_elements(e.variants) AS variant
              WHERE regexp_replace(lower(variant->>'version_id'), '(^urn:uuid:|[{}-])', '', 'g') = replace(v.id::text, '-', '')
          )
      )
  )
  AND NOT EXISTS (
      SELECT 1 FROM agent_versions AS av
      JOIN agents AS a ON a.id = av.agent_id
      WHERE a.workspace_id = sqlc.arg(workspace_id)::text AND replace(v.id::text, '-', '') IN (
          regexp_replace(lower(av.snapshot_json::jsonb->>'system_prompt_version_id'), '(^urn:uuid:|[{}-])', '', 'g'),
          regexp_replace(lower(av.snapshot_json::jsonb->>'user_prompt_version_id'), '(^urn:uuid:|[{}-])', '', 'g')
      )
  )
  AND NOT EXISTS (
      SELECT 1 FROM conversations AS c
      WHERE c.workspace_id = sqlc.arg(workspace_id)::text AND replace(v.id::text, '-', '') IN (
          regexp_replace(lower(c.runtime_config_json->>'system_prompt_version_id'), '(^urn:uuid:|[{}-])', '', 'g'),
          regexp_replace(lower(c.runtime_config_json->>'user_prompt_version_id'), '(^urn:uuid:|[{}-])', '', 'g')
      )
  );

-- name: LockPromptVersionForArchive :one
SELECT v.is_archived
FROM prompt_versions AS v
WHERE v.prompt_id = sqlc.arg(prompt_id)::uuid
  AND v.id = sqlc.arg(version_id)::uuid
FOR UPDATE OF v;

-- name: LockLivePromptVersion :one
SELECT v.id
FROM prompt_versions AS v
JOIN prompts AS p ON p.id = v.prompt_id
WHERE v.id = sqlc.arg(version_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text
  AND NOT v.is_archived
FOR SHARE OF v;

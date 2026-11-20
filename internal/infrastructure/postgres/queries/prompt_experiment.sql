
-- name: GetActivePromptExperiment :one
SELECT e.*
FROM prompt_experiments AS e
JOIN prompts AS p ON p.id = e.prompt_id
WHERE p.id = sqlc.arg(prompt_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text
  AND e.env = sqlc.arg(env)::text
  AND e.status = 'active';

-- name: UpsertPromptExperiment :one
INSERT INTO prompt_experiments (
    id, prompt_id, env, status, baseline_version_id, candidate_version_id,
    traffic_percent, variants, started_at, completed_at, created_by, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
ON CONFLICT (id) DO UPDATE
SET status = EXCLUDED.status,
    traffic_percent = EXCLUDED.traffic_percent,
    variants = EXCLUDED.variants,
    candidate_version_id = EXCLUDED.candidate_version_id,
    completed_at = EXCLUDED.completed_at,
    updated_at = now()
RETURNING *;

-- name: InsertPromptRolloutEvent :one
INSERT INTO prompt_rollout_events (
    id, experiment_id, action, from_traffic, to_traffic, actor, detail
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: LockPromptExperiment :one
SELECT e.id
FROM prompt_experiments AS e
JOIN prompts AS p ON p.id = e.prompt_id
WHERE e.id = sqlc.arg(experiment_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text
FOR UPDATE;




-- name: MarkPromptExperimentCompleted :one
UPDATE prompt_experiments
SET status = 'completed',
    traffic_percent = 100,
    completed_at = sqlc.arg(completed_at),
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND status = 'active'
RETURNING *;



-- name: GetPromptExperiment :one
SELECT e.*
FROM prompt_experiments AS e
JOIN prompts AS p ON p.id = e.prompt_id
WHERE e.id = sqlc.arg(experiment_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text;

-- name: ListPromptRolloutEvents :many
SELECT ev.*
FROM prompt_rollout_events AS ev
JOIN prompt_experiments AS e ON e.id = ev.experiment_id
JOIN prompts AS p ON p.id = e.prompt_id
WHERE e.id = sqlc.arg(experiment_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text
ORDER BY ev.created_at, ev.id;



-- name: InsertPromptEnvEvent :one
INSERT INTO prompt_env_events (
    id, prompt_id, env, action, from_version_id, to_version_id, actor, detail
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListPromptEnvEvents :many
SELECT ev.*
FROM prompt_env_events AS ev
JOIN prompts AS p ON p.id = ev.prompt_id
WHERE ev.prompt_id = sqlc.arg(prompt_id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text
  AND ev.env = sqlc.arg(env)::text
ORDER BY ev.created_at DESC, ev.id;
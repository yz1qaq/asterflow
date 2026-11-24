-- name: CreateProviderAccount :one
INSERT INTO providers (
    id, workspace_id, name, kind, base_url,
    api_key_ciphertext, status, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListProviderAccounts :many
SELECT *
FROM providers
WHERE workspace_id = $1
ORDER BY created_at ASC, id ASC;

-- name: GetProviderAccount :one
SELECT *
FROM providers
WHERE id = $1;

-- name: UpdateProviderAPIKey :execrows
UPDATE providers
SET api_key_ciphertext = $2,
    updated_at = now()
WHERE id = $1;



-- name: CreateDeployment :one
INSERT INTO model_deployments (
    id, workspace_id, provider_account_id, name,
    model_name, region, timeout_ms, max_retries,
    input_price_per_million, output_price_per_million,
    cached_input_price_per_million, status, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: ListDeployments :many
SELECT *
FROM model_deployments
WHERE workspace_id = $1
ORDER BY created_at ASC, id ASC;

-- name: GetDeployment :one
SELECT *
FROM model_deployments
WHERE id = $1;

-- name: UpdateDeploymentPricing :execrows
UPDATE model_deployments
SET input_price_per_million = $2,
    output_price_per_million = $3,
    cached_input_price_per_million = $4,
    updated_at = now()
WHERE id = $1;




-- name: CreateProfile :one
INSERT INTO model_profiles (
    id, workspace_id, name, description, created_by
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListProfiles :many
SELECT *
FROM model_profiles
WHERE workspace_id = $1
ORDER BY created_at ASC, id ASC;

-- name: GetProfile :one
SELECT *
FROM model_profiles
WHERE id = $1;

-- name: LockModelProfile :one
SELECT *
FROM model_profiles
WHERE id = $1
FOR UPDATE;



-- name: NextModelProfileVersion :one
SELECT (COALESCE(MAX(version), 0)::bigint + 1)::bigint AS next_version
FROM model_profile_versions
WHERE profile_id = $1;

-- name: CreateProfileVersion :one
INSERT INTO model_profile_versions (
    id, profile_id, version, primary_deployment_id,
    fallback_deployment_ids, classification_max, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListProfileVersions :many
SELECT *
FROM model_profile_versions
WHERE profile_id = $1
ORDER BY version ASC;

-- name: GetProfileVersion :one
SELECT *
FROM model_profile_versions
WHERE id = $1;






-- name: ListWorkspaceProfileVersions :many
SELECT v.*
FROM model_profile_versions AS v
JOIN model_profiles AS p ON p.id = v.profile_id
WHERE p.workspace_id = $1
ORDER BY p.name ASC, v.version ASC;

-- name: UpsertProjectBinding :one
INSERT INTO project_model_bindings (
    workspace_id, env, model_profile_version_id,
    monthly_budget, rpm_limit, tpm_limit
)
SELECT p.workspace_id, sqlc.arg(env)::text, v.id,
       sqlc.arg(monthly_budget)::numeric,
       sqlc.arg(rpm_limit)::int, sqlc.arg(tpm_limit)::int
FROM model_profile_versions AS v
JOIN model_profiles AS p ON p.id = v.profile_id
WHERE p.workspace_id = sqlc.arg(workspace_id)::text
  AND v.id = sqlc.arg(model_profile_version_id)::uuid
ON CONFLICT (workspace_id, env) DO UPDATE
SET model_profile_version_id = EXCLUDED.model_profile_version_id,
    monthly_budget = EXCLUDED.monthly_budget,
    rpm_limit = EXCLUDED.rpm_limit,
    tpm_limit = EXCLUDED.tpm_limit,
    updated_at = now()
RETURNING *;

-- name: GetProjectBinding :one
SELECT *
FROM project_model_bindings
WHERE workspace_id = $1 AND env = $2;






-- name: HasBudgetBindingForDeployment :one
SELECT EXISTS (
    SELECT 1
    FROM project_model_bindings AS b
    JOIN model_profile_versions AS v
      ON v.id = b.model_profile_version_id
    WHERE b.workspace_id = sqlc.arg(workspace_id)::text
      AND b.monthly_budget > 0
      AND (
          v.primary_deployment_id = sqlc.arg(deployment_id)::uuid
          OR v.fallback_deployment_ids ? (sqlc.arg(deployment_id)::uuid)::text
      )
) AS has_budget_binding;


-- name: LockProviderAccount :one
SELECT id
FROM providers
WHERE id = sqlc.arg(id)::uuid AND workspace_id = sqlc.arg(workspace_id)::text
FOR UPDATE;

-- name: DeleteUnusedProviderAccount :execrows
DELETE FROM providers AS p
WHERE p.id = sqlc.arg(id)::uuid
  AND p.workspace_id = sqlc.arg(workspace_id)::text
  AND NOT EXISTS (
      SELECT 1 FROM model_deployments AS d WHERE d.provider_account_id = p.id
  );

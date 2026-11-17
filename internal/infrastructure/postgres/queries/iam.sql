-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE email = $1
LIMIT 1;

-- name: GetUserByID :one
SELECT *
FROM users
WHERE id = $1
LIMIT 1;

-- name: CreateUser :one
INSERT INTO users (
    id,
    email,
    password_hash,
    name,
    role,
    status,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, now(), now())
RETURNING *;

-- name: ListUsers :many
SELECT *
FROM users
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: SetUserRole :exec
UPDATE users
SET role = $2,
    updated_at = now()
WHERE id = $1;



-- name: GetWorkspaceByID :one
SELECT *
FROM workspaces
WHERE id = $1
LIMIT 1;

-- name: CreateWorkspace :one
INSERT INTO workspaces (
    id,
    name,
    description,
    parent_id,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, now(), now())
RETURNING *;

-- name: ListWorkspaces :many
SELECT *
FROM workspaces
ORDER BY created_at DESC, id DESC
LIMIT $1 OFFSET $2;

-- name: ListUserWorkspaces :many
SELECT w.*
FROM workspaces w
JOIN workspace_members wm ON wm.workspace_id = w.id
WHERE wm.user_id = $1
ORDER BY w.created_at DESC, w.id DESC
LIMIT $2 OFFSET $3;


-- name: UpsertWorkspaceMember :one
INSERT INTO workspace_members (
    workspace_id,
    user_id,
    role
)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, user_id)
DO UPDATE SET role = EXCLUDED.role
RETURNING *;


-- name: GetWorkspaceMember :one
SELECT wm.workspace_id, wm.user_id, u.email, u.name, wm.role, wm.created_at
FROM workspace_members wm
JOIN users u ON u.id = wm.user_id
WHERE wm.workspace_id = $1 AND wm.user_id = $2
LIMIT 1;

-- name: ListWorkspaceMembers :many
SELECT wm.workspace_id, wm.user_id, u.email, u.name, wm.role, wm.created_at
FROM workspace_members wm
JOIN users u ON u.id = wm.user_id
WHERE wm.workspace_id = $1
ORDER BY wm.created_at ASC, wm.user_id ASC;


-- name: LockWorkspace :one
SELECT id
FROM workspaces
WHERE id = $1
FOR UPDATE;

-- name: CountWorkspaceOwners :one
SELECT COUNT(*)
FROM workspace_members
WHERE workspace_id = $1 AND role = 'owner';


-- name: DeleteWorkspaceMember :execrows
DELETE FROM workspace_members
WHERE workspace_id = $1 AND user_id = $2;
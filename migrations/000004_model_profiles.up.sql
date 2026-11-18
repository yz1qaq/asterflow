CREATE TABLE model_profiles (
    id UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE model_profile_versions (
    id UUID PRIMARY KEY,
    profile_id UUID NOT NULL REFERENCES model_profiles(id) ON DELETE CASCADE,
    version INT NOT NULL CHECK (version > 0),
    primary_deployment_id UUID NOT NULL REFERENCES model_deployments(id),
    fallback_deployment_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    classification_max TEXT NOT NULL DEFAULT 'internal',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (profile_id, version),
    CONSTRAINT model_profile_fallbacks_array CHECK (
        jsonb_typeof(fallback_deployment_ids) = 'array'
    )
);
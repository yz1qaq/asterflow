CREATE TABLE prompts (
    id UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX prompts_workspace_name ON prompts (workspace_id, name);

CREATE TABLE prompt_versions (
    id UUID PRIMARY KEY,
    prompt_id UUID NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
    version INT NOT NULL CHECK (version > 0),
    template TEXT NOT NULL,
    variables_schema TEXT NOT NULL DEFAULT '{}',
    model_profile_version_id UUID REFERENCES model_profile_versions(id),
    generation_config JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(generation_config) = 'object'),
    hash TEXT NOT NULL DEFAULT '',
    token_estimate INT NOT NULL DEFAULT 0 CHECK (token_estimate >= 0),
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (prompt_id, version),
    UNIQUE (prompt_id, id)
);

CREATE TABLE prompt_envs (
    prompt_id UUID NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
    env TEXT NOT NULL CHECK (env <> '' AND env = btrim(env)),
    version_id UUID NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (prompt_id, env),
    FOREIGN KEY (prompt_id, version_id)
        REFERENCES prompt_versions(prompt_id, id)
);
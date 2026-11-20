CREATE TABLE agents (
    id UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL CHECK (workspace_id <> '' AND workspace_id = btrim(workspace_id)),
    name TEXT NOT NULL CHECK (name <> '' AND name = btrim(name)),
    description TEXT NOT NULL DEFAULT '',
    template TEXT NOT NULL DEFAULT 'general',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX agents_workspace_name ON agents (workspace_id, name);

CREATE TABLE agent_versions (
    id UUID PRIMARY KEY,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    version INT NOT NULL CHECK (version > 0),
    snapshot_json TEXT NOT NULL DEFAULT '{}'
        CHECK (pg_input_is_valid(snapshot_json, 'jsonb')),
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, version),
    UNIQUE (agent_id, id)
);

CREATE INDEX agent_versions_agent_ver ON agent_versions (agent_id, version DESC);

CREATE TABLE agent_envs (
    agent_id UUID NOT NULL,
    env TEXT NOT NULL CHECK (env <> '' AND env = btrim(env)),
    version_id UUID NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, env),
    FOREIGN KEY (agent_id, version_id) REFERENCES agent_versions(agent_id, id)
);
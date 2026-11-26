
CREATE TABLE tools (
    id UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    source_type TEXT NOT NULL
        CHECK (source_type IN ('rest_api', 'mcp_server', 'internal_sdk', 'code_execution', 'a2a')),
    sensitive BOOLEAN NOT NULL DEFAULT false,
    classification_max TEXT NOT NULL DEFAULT 'internal',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX tools_workspace_name_unique ON tools (workspace_id, name);

CREATE TABLE tool_versions (
    id UUID PRIMARY KEY,
    tool_id UUID NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    version INT NOT NULL,
    schema_json TEXT NOT NULL DEFAULT '{}',
    endpoint_config TEXT NOT NULL DEFAULT '{}',
    auth_config TEXT NOT NULL DEFAULT '{}',
    auth_config_encrypted BYTEA,
    retry_policy TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'draft',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tool_id, version)
);

CREATE INDEX tool_versions_tool_ver ON tool_versions (tool_id, version DESC);
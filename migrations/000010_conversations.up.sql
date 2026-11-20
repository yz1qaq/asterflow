CREATE TABLE conversations (
    id UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL CHECK (workspace_id <> '' AND workspace_id = btrim(workspace_id)),
    agent_id UUID NOT NULL REFERENCES agents(id),
    -- composite FK below guarantees the version belongs to agent_id
    agent_version_id UUID NOT NULL,
    user_id TEXT NOT NULL CHECK (user_id <> ''),
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'running', 'completed', 'error')),
    classification TEXT NOT NULL DEFAULT 'internal'
        CHECK (classification IN ('public', 'internal', 'confidential', 'secret')),
    runtime_config_json JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(runtime_config_json) = 'object'),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    turn_token UUID,
    turn_lease_until TIMESTAMPTZ,
    turn_revision BIGINT NOT NULL DEFAULT 0,
    FOREIGN KEY (agent_id, agent_version_id) REFERENCES agent_versions(agent_id, id)
);

CREATE INDEX conversations_owner
    ON conversations (workspace_id, user_id, started_at DESC);
CREATE INDEX conversations_agent
    ON conversations (workspace_id, agent_id, started_at DESC);
CREATE INDEX conversations_turn_recovery
    ON conversations (turn_lease_until)
    WHERE status = 'running';

CREATE TABLE messages (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    content TEXT NOT NULL DEFAULT '',
    tool_calls JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(tool_calls) = 'array'),
    tool_call_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX messages_conversation ON messages (conversation_id, created_at, id);

CREATE TABLE tool_calls (
    id UUID PRIMARY KEY,
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tool_id UUID,
    tool_version_id UUID,
    args JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(args) = 'object'),
    result JSONB,
    latency_ms INT NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX tool_calls_message ON tool_calls (message_id);
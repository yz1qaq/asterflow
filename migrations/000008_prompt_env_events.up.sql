CREATE TABLE prompt_env_events (
    id UUID PRIMARY KEY,
    prompt_id UUID NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
    env TEXT NOT NULL CHECK (env <> '' AND env = btrim(env)),
    action TEXT NOT NULL CHECK (action IN ('published', 'rolled_back')),
    from_version_id UUID,
    to_version_id UUID NOT NULL,
    actor TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (prompt_id, from_version_id)
        REFERENCES prompt_versions(prompt_id, id),
    FOREIGN KEY (prompt_id, to_version_id)
        REFERENCES prompt_versions(prompt_id, id)
);

CREATE INDEX prompt_env_events_prompt_env
    ON prompt_env_events (prompt_id, env, created_at DESC);
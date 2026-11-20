CREATE TABLE prompt_experiments (
    id UUID PRIMARY KEY,
    prompt_id UUID NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
    env TEXT NOT NULL CHECK (env <> '' AND env = btrim(env)),
    status TEXT NOT NULL CHECK (status IN ('active', 'completed', 'rolled_back', 'stopped')),
    baseline_version_id UUID,
    candidate_version_id UUID,
    traffic_percent INT NOT NULL DEFAULT 0 CHECK (traffic_percent BETWEEN 0 AND 100),
    variants JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(variants) = 'array'),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (prompt_id, baseline_version_id)
        REFERENCES prompt_versions(prompt_id, id),
    FOREIGN KEY (prompt_id, candidate_version_id)
        REFERENCES prompt_versions(prompt_id, id),
    CHECK (candidate_version_id IS NULL OR candidate_version_id <> baseline_version_id)
);

CREATE UNIQUE INDEX prompt_experiments_one_active
    ON prompt_experiments (prompt_id, env)
    WHERE status = 'active';
CREATE INDEX prompt_experiments_prompt_env
    ON prompt_experiments (prompt_id, env, created_at DESC);

CREATE TABLE prompt_rollout_events (
    id UUID PRIMARY KEY,
    experiment_id UUID NOT NULL REFERENCES prompt_experiments(id) ON DELETE CASCADE,
    action TEXT NOT NULL CHECK (action <> ''),
    from_traffic INT NOT NULL DEFAULT 0 CHECK (from_traffic BETWEEN 0 AND 100),
    to_traffic INT NOT NULL DEFAULT 0 CHECK (to_traffic BETWEEN 0 AND 100),
    actor TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX prompt_rollout_events_experiment
    ON prompt_rollout_events (experiment_id, created_at);
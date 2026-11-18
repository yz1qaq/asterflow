CREATE TABLE project_model_bindings (
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    env TEXT NOT NULL CHECK (env <> '' AND env = btrim(env)),
    model_profile_version_id UUID NOT NULL REFERENCES model_profile_versions(id),
    monthly_budget NUMERIC NOT NULL DEFAULT 0
        CHECK (monthly_budget >= 0 AND monthly_budget < 'Infinity'::numeric),
    rpm_limit INT NOT NULL DEFAULT 0 CHECK (rpm_limit >= 0),
    tpm_limit INT NOT NULL DEFAULT 0 CHECK (tpm_limit >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, env)
);
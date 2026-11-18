CREATE TABLE providers (
    id UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    base_url TEXT NOT NULL,
    api_key_ciphertext BYTEA,
    status TEXT NOT NULL DEFAULT 'active',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name),
    UNIQUE (id, workspace_id)
);

CREATE TABLE model_deployments (
    id UUID PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider_account_id UUID NOT NULL,
    name TEXT NOT NULL,
    model_name TEXT NOT NULL,
    region TEXT NOT NULL DEFAULT '',
    timeout_ms INT NOT NULL DEFAULT 120000 CHECK (timeout_ms > 0),
    max_retries INT NOT NULL DEFAULT 1 CHECK (max_retries >= 0),
    input_price_per_million NUMERIC NOT NULL DEFAULT 0,
    output_price_per_million NUMERIC NOT NULL DEFAULT 0,
    cached_input_price_per_million NUMERIC NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name),
    FOREIGN KEY (provider_account_id, workspace_id)
        REFERENCES providers(id, workspace_id) ON DELETE CASCADE,
    CONSTRAINT model_deployment_prices_nonnegative CHECK (
        input_price_per_million >= 0
        AND output_price_per_million >= 0
        AND cached_input_price_per_million >= 0
    )
);
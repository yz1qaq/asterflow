ALTER TABLE tool_versions
    ADD CONSTRAINT tool_versions_tool_id_id_unique UNIQUE (tool_id, id);

CREATE TABLE tool_test_runs (
    id UUID PRIMARY KEY,
    tool_id UUID NOT NULL,
    tool_version_id UUID NOT NULL,
    input TEXT NOT NULL DEFAULT '',
    output TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('success', 'error')),
    latency_ms INT NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- 试跑的工具 ID 和版本 ID 必须指向同一条版本记录。
    FOREIGN KEY (tool_id, tool_version_id)
        REFERENCES tool_versions(tool_id, id) ON DELETE CASCADE,

    CHECK ((status = 'success' AND error IS NULL)
        OR (status = 'error' AND error IS NOT NULL))
);

CREATE INDEX tool_test_runs_version_status_created_at
    ON tool_test_runs (tool_version_id, status, created_at DESC);
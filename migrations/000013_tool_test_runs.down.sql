DROP TABLE tool_test_runs;

ALTER TABLE tool_versions
    DROP CONSTRAINT tool_versions_tool_id_id_unique;
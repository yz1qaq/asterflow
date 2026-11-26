package domain

import "time"

type Tool struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	SourceType  string    `json:"source_type"`
	Description string    `json:"description"`
	Sensitive   bool      `json:"sensitive"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ToolIdentity 是本轮实际加载的工具主记录和版本标识。
type ToolIdentity struct {
	ToolID    string
	VersionID string
}

type ToolVersion struct {
	ID                     string    `json:"id"`
	ToolID                 string    `json:"tool_id"`
	Version                int       `json:"version"`
	SchemaJSON             string    `json:"schema_json"`
	EndpointConfig         string    `json:"endpoint_config"`
	EndpointConfigRedacted bool      `json:"endpoint_config_redacted,omitempty"`
	AuthConfig             string    `json:"auth_config"`
	AuthConfigEncrypted    []byte    `json:"-"`
	HasAuth                bool      `json:"has_auth"`
	RetryPolicy            string    `json:"retry_policy"`
	Status                 string    `json:"status"`
	CreatedBy              string    `json:"created_by"`
	CreatedAt              time.Time `json:"created_at"`
}

type ToolTestRun struct {
	ID            string    `json:"id"`
	ToolID        string    `json:"tool_id"`
	ToolVersionID string    `json:"tool_version_id"`
	Input         string    `json:"input"`
	Output        string    `json:"output"`
	Status        string    `json:"status"`
	LatencyMs     int       `json:"latency_ms"`
	Error         *string   `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

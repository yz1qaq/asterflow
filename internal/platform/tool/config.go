package tool

import (
	"encoding/json"
	"fmt"
	"regexp"
)

type CreateToolRequest struct {
	WorkspaceID    string `json:"-"`
	CreatedBy      string `json:"-"`
	Name           string `json:"name"`
	SourceType     string `json:"source_type"`
	Description    string `json:"description"`
	SchemaJSON     string `json:"schema_json"`
	EndpointConfig string `json:"endpoint_config"`
	AuthConfig     string `json:"auth_config"`
	Sensitive      bool   `json:"sensitive"`
}

// 保存解析后的工具配置
type ToolConfig struct {
	SourceType     string         `json:"source_type"`
	Schema         map[string]any `json:"schema"`
	EndpointConfig map[string]any `json:"endpoint_config"`
	AuthConfig     map[string]any `json:"auth_config"`
}

var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func isValidSourceType(sourceType string) bool {
	switch sourceType {
	case "rest_api", "mcp_server", "internal_sdk", "code_execution", "a2a":
		return true
	default:
		return false
	}
}

func toolConfigFromStrings(sourceType, schemaJSON, endpointJSON, authJSON string) (*ToolConfig, error) {
	schema, err := parseJSONObject("schema_json", schemaJSON)
	if err != nil {
		return nil, err
	}
	endpoint, err := parseJSONObject("endpoint_config", endpointJSON)
	if err != nil {
		return nil, err
	}
	auth, err := parseJSONObject("auth_config", authJSON)
	if err != nil {
		return nil, err
	}
	return &ToolConfig{
		SourceType:     sourceType,
		Schema:         schema,
		EndpointConfig: endpoint,
		AuthConfig:     auth,
	}, nil
}

func parseJSONObject(field, value string) (map[string]any, error) {
	if value == "" {
		return map[string]any{}, nil
	}

	var object map[string]any
	if err := json.Unmarshal([]byte(value), &object); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", field, err)
	}
	if object == nil {
		return nil, fmt.Errorf("invalid %s: expected JSON object", field)
	}
	return object, nil
}

type CreateToolVersionRequest struct {
	SchemaJSON     string `json:"schema_json"`
	EndpointConfig string `json:"endpoint_config"`
	AuthConfig     string `json:"auth_config"`
	RetryPolicy    string `json:"retry_policy"`
	CreatedBy      string `json:"-"`
}

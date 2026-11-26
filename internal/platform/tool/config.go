package tool

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
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

// validateToolEndpointKeys 检查已解析为对象的端点配置中，公开地址字段是否唯一。
func validateToolEndpointKeys(raw string) error {
	if raw == "" {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("invalid endpoint_config: %w", err)
	}

	// 逐个读顶层键，保留 map 解码时会被合并的同名字段。
	seen := make(map[string]struct{}, 2)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("invalid endpoint_config: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("invalid endpoint_config: expected a field name")
		}
		if key == "url" || key == "card_url" {
			if _, exists := seen[key]; exists {
				return fmt.Errorf("endpoint_config has duplicate %s field", key)
			}
			seen[key] = struct{}{}
		}

		// 跳过当前字段的整个值，下一次读取的仍是顶层键。
		var ignored json.RawMessage
		if err := decoder.Decode(&ignored); err != nil {
			return fmt.Errorf("invalid endpoint_config: %w", err)
		}
	}
	return nil
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

package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCreateToolConfig(t *testing.T) {
	var req CreateToolRequest
	if err := json.Unmarshal([]byte(`{"workspace_id":"other","created_by":"other","name":"get_order","source_type":"rest_api"}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.WorkspaceID != "" || req.CreatedBy != "" || !toolNamePattern.MatchString(req.Name) || !isValidSourceType(req.SourceType) {
		t.Fatalf("unexpected request: %+v", req)
	}
	if toolNamePattern.MatchString("查询 订单") || isValidSourceType("unknown") {
		t.Fatal("unexpected acceptance of invalid tool identity")
	}

	config, err := toolConfigFromStrings(req.SourceType, `{"type":"object"}`, `{"method":"POST"}`, "")
	if err != nil || config.SourceType != req.SourceType || config.Schema["type"] != "object" ||
		config.EndpointConfig["method"] != "POST" || config.AuthConfig == nil || len(config.AuthConfig) != 0 {
		t.Fatalf("unexpected tool config: %+v, %v", config, err)
	}

	for _, test := range []struct {
		schema, endpoint, auth string
		field                  string
	}{
		{schema: "{", field: "schema_json"},
		{endpoint: "null", field: "endpoint_config"},
		{auth: "[]", field: "auth_config"},
	} {
		_, err := toolConfigFromStrings("rest_api", test.schema, test.endpoint, test.auth)
		if err == nil || !strings.Contains(err.Error(), test.field) {
			t.Fatalf("want field %q error, got %v", test.field, err)
		}
	}
}

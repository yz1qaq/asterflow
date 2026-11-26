package tool

import (
	"context"
	"testing"

	"github.com/yz1qaq/asterflow/internal/domain"
	"github.com/yz1qaq/asterflow/internal/platform/modelconfig"
)

func TestToolRegistrationSecurity(t *testing.T) {
	ctx := context.Background()
	policy := NewEndpointPolicy([]string{"crossborder-sim"}, false)
	cfg, err := toolConfigFromStrings("rest_api", "{}", `{"url":"http://crossborder-sim:8091/tools/get_order","method":"POST"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.validateConfig(ctx, cfg.SourceType, cfg.EndpointConfig); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		source string
		config map[string]any
	}{
		{source: "rest_api", config: map[string]any{}},
		{source: "rest_api", config: map[string]any{"url": "invalid"}},
		{source: "rest_api", config: map[string]any{"url": "http://127.0.0.1:5432/"}},
		{source: "mcp_server", config: map[string]any{"transport": "stdio"}},
	} {
		if err := policy.validateConfig(ctx, test.source, test.config); err == nil {
			t.Fatalf("expected registration policy error for %+v", test)
		}
	}

	service := NewService(nil)
	version := &domain.ToolVersion{AuthConfig: `{"bearer_token":"fixture-only"}`}
	if err := service.prepareVersionAuth(version); err == nil || version.AuthConfigEncrypted != nil {
		t.Fatal("a configured credential requires a cipher")
	}
	cipher, err := modelconfig.NewCipher([]byte("fixture-secret-only"))
	if err != nil {
		t.Fatal(err)
	}
	service.ConfigureSecurity(cipher, policy)
	plain := version.AuthConfig
	if err := service.prepareVersionAuth(version); err != nil {
		t.Fatal(err)
	}
	if !version.HasAuth || version.AuthConfig != "{}" {
		t.Fatal("expected only ciphertext and metadata")
	}
	decoded, err := cipher.Decrypt(version.AuthConfigEncrypted)
	if err != nil || decoded != plain {
		t.Fatal("tool credential failed round trip")
	}
}

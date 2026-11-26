package tool

import (
	"context"
	"fmt"
	"strings"
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

func TestConfiguredToolURLMustBeSafeToPublish(t *testing.T) {
	ctx := context.Background()
	policy := NewEndpointPolicy([]string{"crossborder-sim"}, false)
	const cleanURL = "http://crossborder-sim:8091/tools/get_order"
	for _, suffix := range []string{"?region=us", "?", "#section"} {
		t.Run(suffix, func(t *testing.T) {
			raw := cleanURL + suffix
			if err := policy.validateConfig(ctx, "rest_api", map[string]any{"url": raw}); err == nil {
				t.Error("stored endpoint must exclude query and fragment")
			}
			version := &domain.ToolVersion{EndpointConfig: fmt.Sprintf(`{"url":%q,"method":"POST"}`, raw)}
			if public := publicToolVersion(version); public.EndpointConfig != "{}" || !public.EndpointConfigRedacted {
				t.Error("public endpoint config must omit private URL components")
			}
			if !strings.Contains(version.EndpointConfig, raw) {
				t.Error("redaction changed the stored input")
			}
		})
	}
	if err := policy.validateToolEndpoints(ctx, "rest_api", map[string]any{"url": cleanURL + "?region=us"}, parseToolEndpointURL); err != nil {
		t.Fatalf("stored version must keep its runtime endpoint: %v", err)
	}
	if err := policy.ValidateURL(ctx, cleanURL+"?region=us"); err != nil {
		t.Fatalf("outbound validation should preserve existing redirect behavior: %v", err)
	}
	version := &domain.ToolVersion{EndpointConfig: fmt.Sprintf(`{"url":%q,"method":"POST"}`, cleanURL)}
	if public := publicToolVersion(version); public.EndpointConfig != version.EndpointConfig || public.EndpointConfigRedacted {
		t.Error("ordinary endpoint config should remain readable")
	}
}

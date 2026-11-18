package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("ASTERFLOW_CREDENTIAL_SECRET_KEY", "credential-fixture-key-at-least-32-bytes")
	t.Setenv("ASTERFLOW_JWT_SECRET_KEY", "config-fixture-key-at-least-32-bytes")
	t.Setenv("ASTERFLOW_AUTOSEED_ADMIN", "false")
	t.Setenv("ASTERFLOW_DATABASE_URL", "postgres://unit@127.0.0.1:1/unit?sslmode=disable")
	cases := []struct {
		name    string
		set     bool
		addr    string
		want    string
		wantErr bool
	}{
		{"default", false, "", ":18090", false},
		{"wildcard", true, ":8080", ":8080", false},
		{"ipv4", true, "127.0.0.1:18090", "127.0.0.1:18090", false},
		{"ipv6", true, "[::1]:18090", "[::1]:18090", false},
		{"hostname_without_dns", true, "unresolvable.invalid:18090", "unresolvable.invalid:18090", false},
		{"max_port", true, ":65535", ":65535", false},
		{"empty", true, "", "", true},
		{"spaces", true, "  ", "", true},
		{"space_in_host", true, "local host:18090", "", true},
		{"unicode_space", true, "\u3000:18090", "", true},
		{"missing_port", true, "localhost", "", true},
		{"url", true, "http://localhost:18090", "", true},
		{"unbracketed_ipv6", true, "::1:18090", "", true},
		{"zero_port", true, ":0", "", true},
		{"too_large", true, ":65536", "", true},
		{"negative", true, ":-1", "", true},
		{"plus_sign", true, ":+80", "", true},
		{"named_port", true, ":http", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ASTERFLOW_HTTP_ADDR", tc.addr)
			if !tc.set {
				if err := os.Unsetenv("ASTERFLOW_HTTP_ADDR"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Load()
			if tc.wantErr {
				if err == nil || got != (Config{}) {
					t.Fatalf("want error and zero config, got %#v, %v", got, err)
				}
				return
			}
			if err != nil || got.HTTPAddr != tc.want {
				t.Fatalf("want %q, got %#v, %v", tc.want, got, err)
			}
		})
	}
}

func TestLoadDatabaseURL(t *testing.T) {
	t.Setenv("ASTERFLOW_CREDENTIAL_SECRET_KEY", "credential-fixture-key-at-least-32-bytes")
	t.Setenv("ASTERFLOW_JWT_SECRET_KEY", "config-fixture-key-at-least-32-bytes")
	t.Setenv("ASTERFLOW_AUTOSEED_ADMIN", "false")
	cases := []struct {
		name    string
		set     bool
		value   string
		wantErr bool
	}{
		{"missing", false, "", true},
		{"empty", true, "", true},
		{"spaces", true, "  \t\n", true},
		{"unicode_space", true, "\u3000", true},
		{"uri", true, "postgres://unit@127.0.0.1:1/unit?sslmode=disable", false},
		{"keyword_configuration", true, "host=127.0.0.1 port=1 dbname=unit user=unit", false},
		{"defer_parsing_to_postgres", true, "invalid-but-nonempty", false},
		{"preserve_original_value", true, " host=127.0.0.1 dbname=unit ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ASTERFLOW_HTTP_ADDR", ":18090")
			t.Setenv("ASTERFLOW_DATABASE_URL", tc.value)
			if !tc.set {
				if err := os.Unsetenv("ASTERFLOW_DATABASE_URL"); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load()
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "ASTERFLOW_DATABASE_URL") {
					t.Fatalf("want missing database configuration error, got %v", err)
				}
				if cfg != (Config{}) {
					t.Fatal("configuration error returned a partial config")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected configuration error: %v", err)
			}
			if cfg.DatabaseURL != tc.value {
				t.Fatal("Load did not preserve the original database configuration")
			}
		})
	}
}

func TestLoadAuthConfiguration(t *testing.T) {
	t.Setenv("ASTERFLOW_CREDENTIAL_SECRET_KEY", "credential-fixture-key-at-least-32-bytes")
	for _, tc := range []struct {
		name, key, seed, email, password string
		unsetSeed, wantErr               bool
	}{
		{"valid defaults", strings.Repeat("x", 32), "", "", "", true, false},
		{"missing key", "", "false", "", "", false, true},
		{"short key", "private-short-key", "false", "", "", false, true},
		{"blank key", strings.Repeat(" ", 32), "false", "", "", false, true},
		{"invalid flag", strings.Repeat("x", 32), "sometimes", "", "", false, true},
		{"seed credentials", strings.Repeat("x", 32), "true", " admin@example.test ", " password123 ", false, false},
		{"missing email", strings.Repeat("x", 32), "true", " ", "password123", false, true},
		{"missing password", strings.Repeat("x", 32), "true", "admin@example.test", "", false, true},
		{"disabled seed", strings.Repeat("x", 32), "false", "", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ASTERFLOW_HTTP_ADDR", ":18090")
			t.Setenv("ASTERFLOW_DATABASE_URL", "host=unit dbname=unit")
			t.Setenv("ASTERFLOW_JWT_SECRET_KEY", tc.key)
			t.Setenv("ASTERFLOW_AUTOSEED_ADMIN", tc.seed)
			t.Setenv("ASTERFLOW_AUTOSEED_ADMIN_EMAIL", tc.email)
			t.Setenv("ASTERFLOW_AUTOSEED_ADMIN_PASSWORD", tc.password)
			if tc.unsetSeed {
				_ = os.Unsetenv("ASTERFLOW_AUTOSEED_ADMIN")
			}
			cfg, err := Load()
			if tc.wantErr {
				if err == nil || cfg != (Config{}) {
					t.Fatal("invalid auth configuration accepted")
				}
				if tc.key != "" && strings.TrimSpace(tc.key) != "" && strings.Contains(err.Error(), tc.key) {
					t.Fatal("secret leaked in configuration error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.JWTSecretKey != tc.key || cfg.AutoseedAdmin != (tc.seed == "true") ||
				cfg.AutoseedAdminEmail != strings.TrimSpace(tc.email) || cfg.AutoseedAdminPassword != tc.password {
				t.Fatal("auth configuration was not preserved")
			}
		})
	}
}

func TestLoadCredentialSecretKey(t *testing.T) {
	t.Setenv("ASTERFLOW_HTTP_ADDR", ":18090")
	t.Setenv("ASTERFLOW_DATABASE_URL", "host=unit dbname=unit")
	t.Setenv("ASTERFLOW_JWT_SECRET_KEY", strings.Repeat("j", 32))
	t.Setenv("ASTERFLOW_AUTOSEED_ADMIN", "false")
	for _, tc := range []struct {
		name, value    string
		unset, wantErr bool
	}{
		{"missing", "", true, true},
		{"empty", "", false, true},
		{"short", "private-short-fixture", false, true},
		{"blank", strings.Repeat(" ", 32), false, true},
		{"valid", strings.Repeat("c", 32), false, false},
		{"preserve", " " + strings.Repeat("c", 32) + " ", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ASTERFLOW_CREDENTIAL_SECRET_KEY", tc.value)
			if tc.unset {
				if err := os.Unsetenv("ASTERFLOW_CREDENTIAL_SECRET_KEY"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Load()
			if tc.wantErr {
				if err == nil || got != (Config{}) || !strings.Contains(err.Error(), "ASTERFLOW_CREDENTIAL_SECRET_KEY") {
					t.Fatal("invalid credential secret was accepted")
				}
				if strings.TrimSpace(tc.value) != "" && strings.Contains(err.Error(), tc.value) {
					t.Fatal("credential secret leaked in error")
				}
				return
			}
			if err != nil || got.CredentialSecretKey != tc.value {
				t.Fatal("credential secret was not preserved")
			}
		})
	}
}

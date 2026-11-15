package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
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

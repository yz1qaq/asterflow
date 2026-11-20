package promptcache

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCompileAndRender(t *testing.T) {
	raw := "order {{.order_id}}; quantity {{.quantity}}; urgent {{.urgent}}"
	schema := `{"type":"object","required":["urgent","quantity","order_id"],"properties":{"order_id":{"type":"string"}}}`
	c, err := Compile("pv-1", raw, schema)
	if err != nil {
		t.Fatal(err)
	}
	if c.VersionID != "pv-1" || c.EstTokens <= 0 {
		t.Fatal("compiled template lost its version and estimate")
	}
	vars := map[string]any{"order_id": "ORD-1001", "quantity": 0, "urgent": false}
	got, err := c.Render(context.Background(), vars)
	if err != nil || got != "order ORD-1001; quantity 0; urgent false" {
		t.Fatalf("unexpected rendering %q: %v", got, err)
	}
	delete(vars, "order_id")
	got, err = c.Render(context.Background(), vars)
	if got != "" || err == nil || !strings.Contains(err.Error(), `"order_id"`) {
		t.Fatalf("missing required variable should be identified: %v", err)
	}
	// No required list still does not allow an absent dot-lookup.
	c, err = Compile("pv-2", "prefix {{.customer}}", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err = c.Render(context.Background(), nil)
	if got != "" || err == nil {
		t.Fatal("failed render must not return a partial prefix")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Render(ctx, map[string]any{"customer": "A"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("render should preserve cancellation: %v", err)
	}
}

func TestRejectInvalidTemplateOrSchema(t *testing.T) {
	if _, err := Compile("", "{{.order_id", "{}"); err == nil {
		t.Fatal("invalid template syntax was accepted")
	}
	for _, schema := range []string{`{`, `[]`, `null`, `{"required":"order_id"}`, `{"required":null}`, `{"required":[" "]}`} {
		if _, err := Compile("", "order {{.order_id}}", schema); err == nil {
			t.Fatalf("invalid variables schema was accepted: %s", schema)
		}
	}
	for _, schema := range []string{"", "  {} ", `{"required":[]}`} {
		if _, err := Compile("", "literal", schema); err != nil {
			t.Fatalf("valid empty requirements rejected: %v", err)
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens("abcd\u8ba2\u5355"); got != 3 {
		t.Fatalf("expected one approximate token for abcd and two for CJK, got %d", got)
	}
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf("expected 0 for empty input, got %d", got)
	}
}

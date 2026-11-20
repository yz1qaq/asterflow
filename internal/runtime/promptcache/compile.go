package promptcache

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"unicode"
)

type Compiled struct {
	VersionID    string
	EstTokens    int
	requiredVars []string
	tmpl         *template.Template
}

func Compile(versionID, raw, variablesSchemaJSON string) (*Compiled, error) {
	tmpl, err := template.New("prompt").
		Option("missingkey=error").
		Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse prompt template: %w", err)
	}

	required, err := parseRequired(variablesSchemaJSON)
	if err != nil {
		return nil, fmt.Errorf("parse prompt variables schema: %w", err)
	}

	return &Compiled{
		VersionID:    versionID,
		EstTokens:    EstimateTokens(raw),
		requiredVars: required,
		tmpl:         tmpl,
	}, nil
}

//拿到必须字段的切片
func parseRequired(schemaJSON string) ([]string, error) {
	if strings.TrimSpace(schemaJSON) == "" {
		return nil, nil
	}

	var schema map[string]json.RawMessage
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return nil, err
	}
	if schema == nil {
		return nil, fmt.Errorf("variables schema must be a JSON object")
	}

	raw, exists := schema["required"]
	if !exists {
		return nil, nil
	}

	var required []string
	if err := json.Unmarshal(raw, &required); err != nil {
		return nil, err
	}
	if required == nil {
		return nil, fmt.Errorf("required must be an array")
	}
	for _, key := range required {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("required variable name must not be blank")
		}
	}

	sort.Strings(required)
	return required, nil
}

func (c *Compiled) Render(ctx context.Context, vars map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	for _, key := range c.requiredVars {
		if _, ok := vars[key]; !ok {
			return "", fmt.Errorf("missing required prompt variable %q", key)
		}
	}

	var out strings.Builder
	if err := c.tmpl.Execute(&out, vars); err != nil {
		return "", fmt.Errorf("render prompt: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return out.String(), nil
}

//估算token
func EstimateTokens(s string) int {
	var cjk, other int
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r),
			unicode.Is(unicode.Hiragana, r),
			unicode.Is(unicode.Katakana, r),
			unicode.Is(unicode.Hangul, r):
			cjk++
		default:
			other++
		}
	}
	return cjk + (other+3)/4
}

package promptcache

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"unicode"

	einoprompt "github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
)

type Compiled struct {
	VersionID    string
	EstTokens    int
	requiredVars []string
	tmpl         einoprompt.ChatTemplate
}

// Compile checks the template syntax and prepares the Eino template used at runtime.
func Compile(versionID, raw, variablesSchemaJSON string) (*Compiled, error) {
	_, err := template.New("prompt").
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
		tmpl:         einoprompt.FromMessages(schema.GoTemplate, schema.UserMessage(raw)),
	}, nil
}

// 拿到必须字段的切片
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

	messages, err := c.tmpl.Format(ctx, vars)
	if err != nil {
		return "", fmt.Errorf("render prompt: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(messages) != 1 {
		return "", fmt.Errorf("render prompt: expected one message, got %d", len(messages))
	}
	return messages[0].Content, nil
}

// 估算token
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

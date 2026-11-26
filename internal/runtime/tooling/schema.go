package tooling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

type schemaValidatingExecutor struct {
	next   Executor
	schema *gojsonschema.Schema
}

func withSchemaValidation(next Executor, schema map[string]any) (Executor, error) {
	if len(schema) == 0 {
		return next, nil
	}

	// 编译时使用本地配置。文档内引用由库处理，外部加载统一拒绝。
	loader := inlineSchemaLoader{JSONLoader: gojsonschema.NewGoLoader(schema)}
	compiled, err := gojsonschema.NewSchema(loader)
	if err != nil {
		return nil, fmt.Errorf("compile tool input schema: %w", err)
	}
	return &schemaValidatingExecutor{next: next, schema: compiled}, nil
}

func (e *schemaValidatingExecutor) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if !json.Valid(args) {
		return "", errors.New("tool input must be valid JSON")
	}
	result, err := e.schema.Validate(gojsonschema.NewBytesLoader(args))
	if err != nil {
		return "", fmt.Errorf("validate tool input: %w", err)
	}

	// 参数合法才调用实际执行器，校验失败时返回字段问题。
	if !result.Valid() {
		problems := make([]string, 0, len(result.Errors()))
		for _, problem := range result.Errors() {
			problems = append(problems, problem.String())
		}
		return "", fmt.Errorf("tool input violates schema: %s", strings.Join(problems, ", "))
	}
	return e.next.Execute(ctx, args)
}

type inlineSchemaLoader struct {
	gojsonschema.JSONLoader
}

func (inlineSchemaLoader) LoaderFactory() gojsonschema.JSONLoaderFactory {
	return blockedSchemaFactory{}
}

type blockedSchemaFactory struct{}

func (blockedSchemaFactory) New(source string) gojsonschema.JSONLoader {
	return blockedSchemaLoader{JSONLoader: gojsonschema.NewReferenceLoader(source)}
}

type blockedSchemaLoader struct {
	gojsonschema.JSONLoader
}

func (blockedSchemaLoader) LoadJSON() (any, error) {
	return nil, errors.New("external tool schema references are disabled")
}

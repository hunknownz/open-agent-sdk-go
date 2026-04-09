package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hunknownz/open-agent-sdk-go/types"
)

const StructuredOutputToolName = "structured_output"

type structuredOutputTool struct {
	schema map[string]interface{}
}

func newStructuredOutputTool(schema map[string]interface{}) types.Tool {
	return &structuredOutputTool{schema: cloneStructuredSchema(schema)}
}

func (t *structuredOutputTool) Name() string {
	return StructuredOutputToolName
}

func (t *structuredOutputTool) Description() string {
	return "Return the final structured response that satisfies the provided JSON schema."
}

func (t *structuredOutputTool) InputSchema() types.ToolInputSchema {
	return types.ToolInputSchema{
		Type:       "object",
		Properties: cloneStructuredSchema(t.schema),
		Required:   schemaRequiredFields(t.schema),
	}
}

func (t *structuredOutputTool) Call(_ context.Context, input map[string]interface{}, _ *types.ToolUseContext) (*types.ToolResult, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal structured output: %w", err)
	}

	return &types.ToolResult{
		Data: input,
		Content: []types.ContentBlock{{
			Type: types.ContentBlockText,
			Text: string(encoded),
		}},
	}, nil
}

func (t *structuredOutputTool) IsConcurrencySafe(map[string]interface{}) bool {
	return true
}

func (t *structuredOutputTool) IsReadOnly(map[string]interface{}) bool {
	return true
}

func cloneStructuredSchema(schema map[string]interface{}) map[string]interface{} {
	if len(schema) == 0 {
		return nil
	}
	clone := make(map[string]interface{}, len(schema))
	for key, value := range schema {
		clone[key] = value
	}
	return clone
}

func schemaRequiredFields(schema map[string]interface{}) []string {
	if len(schema) == 0 {
		return nil
	}
	raw, ok := schema["required"]
	if !ok {
		return nil
	}
	values, ok := raw.([]string)
	if ok {
		return append([]string(nil), values...)
	}
	generic, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	required := make([]string, 0, len(generic))
	for _, value := range generic {
		if field, ok := value.(string); ok && field != "" {
			required = append(required, field)
		}
	}
	return required
}

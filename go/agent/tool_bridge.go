package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eino-contrib/jsonschema"

	einotool "github.com/cloudwego/eino/components/tool"
	einoschema "github.com/cloudwego/eino/schema"
)

// einoTool adapts an agent.Tool to Eino's tool.InvokableTool. It builds the
// schema once at construction time from the struct returned by
// Tool.InputSchema() and reuses the resulting *einoschema.ToolInfo for every
// call.
type einoTool struct {
	inner Tool
	info  *einoschema.ToolInfo
}

func newEinoTool(t Tool) (*einoTool, error) {
	info, err := buildToolInfo(t)
	if err != nil {
		return nil, fmt.Errorf("agent: build tool info for %q: %w", t.Name(), err)
	}
	return &einoTool{inner: t, info: info}, nil
}

func (t *einoTool) Info(_ context.Context) (*einoschema.ToolInfo, error) {
	return t.info, nil
}

func (t *einoTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	result, err := t.inner.Execute(ctx, json.RawMessage(argumentsInJSON))
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// buildToolInfo derives an *einoschema.ToolInfo from t.InputSchema(). The
// returned struct value is reflected by eino-contrib/jsonschema using
// `jsonschema:"..."` tags on its fields.
func buildToolInfo(t Tool) (*einoschema.ToolInfo, error) {
	input := t.InputSchema()

	info := &einoschema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
	}

	if input == nil {
		return info, nil
	}

	r := &jsonschema.Reflector{
		Anonymous:      true,
		DoNotReference: true,
	}
	js := r.Reflect(input)
	js.Version = ""

	info.ParamsOneOf = einoschema.NewParamsOneOfByJSONSchema(js)
	return info, nil
}

// bridgeTools converts a slice of agent.Tool to Eino tools. The returned
// einotool.BaseTool slice is what compose.ToolsNodeConfig expects.
func bridgeTools(tools []Tool) ([]einotool.BaseTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]einotool.BaseTool, 0, len(tools))
	for _, t := range tools {
		bridged, err := newEinoTool(t)
		if err != nil {
			return nil, err
		}
		out = append(out, bridged)
	}
	return out, nil
}

// bridgeToolInfos returns just the *einoschema.ToolInfo values, useful when
// binding tools to a model directly (Chat path) without a ToolsNode.
func bridgeToolInfos(tools []Tool) ([]*einoschema.ToolInfo, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]*einoschema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		info, err := buildToolInfo(t)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

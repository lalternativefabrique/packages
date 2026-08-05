package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	einoagent "github.com/cloudwego/eino/flow/agent"
	"github.com/cloudwego/eino/flow/agent/react"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	einoschema "github.com/cloudwego/eino/schema"
)

const (
	defaultMaxSteps    = 5
	defaultToolTimeout = 30 * time.Second
)

// agentImpl wires our Agent contract on top of eino's react.Agent. The ReAct
// loop, tool dispatch, retries inside the loop, and stream tee are all handled
// by Eino — we only translate inputs/outputs and bridge the callback handler.
type agentImpl struct {
	model model.ToolCallingChatModel
	cfg   agentConfig
}

func (a *agentImpl) Run(ctx context.Context, req Request) (*Response, error) {
	react, opts, err := a.buildAgent(ctx, req)
	if err != nil {
		return nil, err
	}
	out, err := react.Generate(ctx, buildEinoMessages(req.System, req.Messages), opts...)
	if err != nil {
		return nil, err
	}
	return einoMessageToResponse(out), nil
}

func (a *agentImpl) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	react, opts, err := a.buildAgent(ctx, req)
	if err != nil {
		return nil, err
	}
	reader, err := react.Stream(ctx, buildEinoMessages(req.System, req.Messages), opts...)
	if err != nil {
		return nil, err
	}
	return agentStreamToEvents(ctx, reader), nil
}

func (a *agentImpl) buildAgent(ctx context.Context, req Request) (*react.Agent, []einoagent.AgentOption, error) {
	maxSteps := req.MaxSteps
	if maxSteps == 0 {
		maxSteps = a.cfg.maxSteps
	}
	if maxSteps == 0 {
		maxSteps = defaultMaxSteps
	}

	tools, err := bridgeTools(req.Tools)
	if err != nil {
		return nil, nil, err
	}

	r, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: a.model,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: tools},
		MaxStep:          maxSteps,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("agent: build react agent: %w", err)
	}

	var opts []einoagent.AgentOption
	if a.cfg.callback != nil {
		opts = append(opts, einoagent.WithComposeOptions(
			compose.WithCallbacks(newEinoCallbackBridge(a.cfg.callback)),
		))
	}
	return r, opts, nil
}

// agentStreamToEvents drains an Eino *StreamReader[*Message] from the React
// agent and emits agent.Event values. Tool start/end events are NOT emitted
// here: they flow through the callback bridge instead, because the React
// graph's stream only carries the assistant's text and tool-call decisions,
// not the tool execution outputs.
func agentStreamToEvents(ctx context.Context, reader *einoschema.StreamReader[*einoschema.Message]) <-chan Event {
	out := make(chan Event, 16)
	go func() {
		defer close(out)
		defer reader.Close()
		for {
			select {
			case <-ctx.Done():
				out <- Event{Kind: EventError, Err: ctx.Err()}
				return
			default:
			}
			chunk, err := reader.Recv()
			if errors.Is(err, io.EOF) {
				out <- Event{Kind: EventDone}
				return
			}
			if err != nil {
				out <- Event{Kind: EventError, Err: err}
				return
			}
			if chunk == nil {
				continue
			}
			if chunk.Content != "" {
				out <- Event{Kind: EventTextDelta, Text: chunk.Content}
			}
			for _, tc := range chunk.ToolCalls {
				out <- Event{
					Kind:     EventToolCallStart,
					ToolName: tc.Function.Name,
					ToolArgs: tc.Function.Arguments,
				}
			}
		}
	}()
	return out
}

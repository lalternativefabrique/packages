package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cloudwego/eino/components/model"
	einoschema "github.com/cloudwego/eino/schema"
)

// chatImpl is the concrete Chat backed by an Eino ToolCallingChatModel.
type chatImpl struct {
	model model.ToolCallingChatModel
}

func (c *chatImpl) Run(ctx context.Context, req Request) (*Response, error) {
	m, err := c.bindTools(req.Tools)
	if err != nil {
		return nil, err
	}
	input := buildEinoMessages(req.System, req.Messages)
	out, err := m.Generate(ctx, input)
	if err != nil {
		return nil, err
	}
	return einoMessageToResponse(out), nil
}

func (c *chatImpl) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	m, err := c.bindTools(req.Tools)
	if err != nil {
		return nil, err
	}
	input := buildEinoMessages(req.System, req.Messages)
	reader, err := m.Stream(ctx, input)
	if err != nil {
		return nil, err
	}
	return streamReaderToEvents(ctx, reader), nil
}

func (c *chatImpl) bindTools(tools []Tool) (model.ToolCallingChatModel, error) {
	if len(tools) == 0 {
		return c.model, nil
	}
	infos, err := bridgeToolInfos(tools)
	if err != nil {
		return nil, err
	}
	bound, err := c.model.WithTools(infos)
	if err != nil {
		return nil, fmt.Errorf("agent: bind tools: %w", err)
	}
	return bound, nil
}

// buildEinoMessages converts our Message slice + system prompt into Eino's
// schema.Message format expected by ToolCallingChatModel.
func buildEinoMessages(system string, msgs []Message) []*einoschema.Message {
	out := make([]*einoschema.Message, 0, len(msgs)+1)
	if system != "" {
		out = append(out, &einoschema.Message{
			Role:    einoschema.System,
			Content: system,
		})
	}
	for _, m := range msgs {
		em := &einoschema.Message{
			Role:       roleToEino(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			em.ToolCalls = append(em.ToolCalls, einoschema.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: einoschema.FunctionCall{
					Name:      tc.Name,
					Arguments: string(tc.Arguments),
				},
			})
		}
		out = append(out, em)
	}
	return out
}

func roleToEino(r Role) einoschema.RoleType {
	switch r {
	case RoleSystem:
		return einoschema.System
	case RoleUser:
		return einoschema.User
	case RoleAssistant:
		return einoschema.Assistant
	case RoleTool:
		return einoschema.Tool
	default:
		return einoschema.User
	}
}

func einoMessageToResponse(m *einoschema.Message) *Response {
	if m == nil {
		return &Response{}
	}
	resp := &Response{Text: m.Content}
	if m.ResponseMeta != nil && m.ResponseMeta.Usage != nil {
		resp.Usage = Usage{
			InputTokens:       m.ResponseMeta.Usage.PromptTokens,
			OutputTokens:      m.ResponseMeta.Usage.CompletionTokens,
			CachedInputTokens: m.ResponseMeta.Usage.PromptTokenDetails.CachedTokens,
		}
	}
	return resp
}

// streamReaderToEvents drains an Eino *StreamReader[*Message] and emits
// agent.Event values. Closes the channel and the reader on completion.
func streamReaderToEvents(ctx context.Context, reader *einoschema.StreamReader[*einoschema.Message]) <-chan Event {
	out := make(chan Event, 8)
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
			if chunk.ResponseMeta != nil && chunk.ResponseMeta.Usage != nil {
				out <- Event{
					Kind:              EventUsage,
					InputTokens:       chunk.ResponseMeta.Usage.PromptTokens,
					OutputTokens:      chunk.ResponseMeta.Usage.CompletionTokens,
					CachedInputTokens: chunk.ResponseMeta.Usage.PromptTokenDetails.CachedTokens,
				}
			}
		}
	}()
	return out
}

// dropUnused keeps json import live in case future serialisation is added.
var _ = json.Marshal

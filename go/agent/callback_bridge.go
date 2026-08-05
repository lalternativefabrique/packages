package agent

import (
	"context"
	"sync/atomic"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
)

// stepCounter is stashed in context to give every model invocation a stable
// iteration number, used by both OnModelStart/End and OnToolStart/End.
type stepCounterKey struct{}

func newStepCounter() *int64 { var n int64; return &n }

// einoCallbackBridge converts the Eino callbacks.Handler stream into the
// higher-level agent.Callback our consumers implement (NATS publisher in
// diagnosis, audit log elsewhere). One bridge per Run/Stream call.
type einoCallbackBridge struct {
	user    Callback
	counter *int64
}

func newEinoCallbackBridge(user Callback) callbacks.Handler {
	bridge := &einoCallbackBridge{user: user, counter: newStepCounter()}
	return callbacks.NewHandlerBuilder().
		OnStartFn(bridge.onStart).
		OnEndFn(bridge.onEnd).
		OnErrorFn(bridge.onError).
		Build()
}

func (b *einoCallbackBridge) currentStep() int {
	return int(atomic.LoadInt64(b.counter))
}

func (b *einoCallbackBridge) onStart(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
	if info == nil || b.user == nil {
		return ctx
	}
	switch info.Component {
	case components.ComponentOfChatModel:
		step := int(atomic.AddInt64(b.counter, 1))
		mi := einomodel.ConvCallbackInput(input)
		if mi == nil {
			return ctx
		}
		b.user.OnModelStart(ctx, step, einoMessagesToOurs(mi.Messages))
	case components.ComponentOfTool:
		ti := einotool.ConvCallbackInput(input)
		if ti == nil {
			return ctx
		}
		b.user.OnToolStart(ctx, b.currentStep(), ToolCall{
			Name:      runInfoName(info),
			Arguments: []byte(ti.ArgumentsInJSON),
		})
	}
	return ctx
}

func (b *einoCallbackBridge) onEnd(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
	if info == nil || b.user == nil {
		return ctx
	}
	switch info.Component {
	case components.ComponentOfChatModel:
		mo := einomodel.ConvCallbackOutput(output)
		if mo == nil {
			return ctx
		}
		resp := &Response{}
		if mo.Message != nil {
			resp.Text = mo.Message.Content
		}
		if mo.TokenUsage != nil {
			resp.Usage = Usage{
				InputTokens:  mo.TokenUsage.PromptTokens,
				OutputTokens: mo.TokenUsage.CompletionTokens,
			}
		}
		b.user.OnModelEnd(ctx, b.currentStep(), resp, 0)
		if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
			b.user.OnUsage(ctx, b.currentStep(), resp.Usage)
		}
		// If the assistant emitted text alongside tool calls, surface it as a
		// "thought" to the consumer (used by diagnosis to publish live
		// reasoning between tool calls).
		if mo.Message != nil && mo.Message.Content != "" && len(mo.Message.ToolCalls) > 0 {
			b.user.OnThought(ctx, b.currentStep(), mo.Message.Content)
		}
	case components.ComponentOfTool:
		to := einotool.ConvCallbackOutput(output)
		if to == nil {
			return ctx
		}
		result := ToolResult{Content: to.Response}
		b.user.OnToolEnd(ctx, b.currentStep(), ToolCall{Name: runInfoName(info)}, result, 0, nil)
	}
	return ctx
}

func (b *einoCallbackBridge) onError(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
	if info == nil || b.user == nil {
		return ctx
	}
	if info.Component == components.ComponentOfTool {
		b.user.OnToolEnd(ctx, b.currentStep(), ToolCall{Name: runInfoName(info)}, ToolResult{}, 0, err)
	}
	return ctx
}

func runInfoName(info *callbacks.RunInfo) string {
	if info == nil {
		return ""
	}
	if info.Name != "" {
		return info.Name
	}
	return info.Type
}

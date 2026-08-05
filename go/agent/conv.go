package agent

import (
	"encoding/json"

	einoschema "github.com/cloudwego/eino/schema"
)

// einoMessagesToOurs converts an Eino message slice into our public Message
// slice. Used by the callback bridge so consumers see the same conversation
// the model saw.
func einoMessagesToOurs(msgs []*einoschema.Message) []Message {
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		ours := Message{
			Role:       einoRoleToOurs(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			ours.ToolCalls = append(ours.ToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			})
		}
		out = append(out, ours)
	}
	return out
}

func einoRoleToOurs(r einoschema.RoleType) Role {
	switch r {
	case einoschema.System:
		return RoleSystem
	case einoschema.User:
		return RoleUser
	case einoschema.Assistant:
		return RoleAssistant
	case einoschema.Tool:
		return RoleTool
	default:
		return RoleUser
	}
}

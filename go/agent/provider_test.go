package agent

import "testing"

func TestNewChatAndAgentAcceptKnownProviders(t *testing.T) {
	if _, err := NewChat(Provider{Kind: "anthropic", APIKey: "k", Model: "m"}); err != nil {
		t.Fatalf("NewChat: %v", err)
	}
	if _, err := NewAgent(Provider{Kind: "openai", APIKey: "k", Model: "m"}, WithMaxSteps(3)); err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if _, err := NewChat(Provider{Kind: "nope", APIKey: "k", Model: "m"}); err == nil {
		t.Fatal("expected an error for an unknown provider kind")
	}
}

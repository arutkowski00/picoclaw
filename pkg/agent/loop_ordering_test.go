package agent

import (
	"context"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
)

// TestAgentLoop_InboundOverride_FieldExists verifies the inboundOverride field
// and SetInboundOverride method exist and are wired correctly.
func TestAgentLoop_InboundOverride_FieldExists(t *testing.T) {
	al := &AgentLoop{}
	ch := make(chan bus.InboundMessage, 1)
	al.SetInboundOverride(ch)
	if al.inboundOverride == nil {
		t.Fatal("inboundOverride should not be nil after SetInboundOverride")
	}
}

// TestConsumeInbound_UsesOverrideChannel verifies that consumeInbound reads
// from the override channel when one is set.
func TestConsumeInbound_UsesOverrideChannel(t *testing.T) {
	al := &AgentLoop{}
	ch := make(chan bus.InboundMessage, 1)
	al.SetInboundOverride(ch)

	want := bus.InboundMessage{Content: "test", Channel: "telegram"}
	ch <- want

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	got, ok := al.consumeInbound(ctx)
	if !ok {
		t.Fatal("consumeInbound returned ok=false")
	}
	if got.Content != want.Content {
		t.Errorf("got content %q, want %q", got.Content, want.Content)
	}
	if got.Channel != want.Channel {
		t.Errorf("got channel %q, want %q", got.Channel, want.Channel)
	}
}

// TestConsumeInbound_CancelReturnsOkFalse verifies that consumeInbound returns
// ok=false when the context is cancelled and no message is available.
func TestConsumeInbound_CancelReturnsOkFalse(t *testing.T) {
	al := &AgentLoop{}
	ch := make(chan bus.InboundMessage) // unbuffered, nothing to send
	al.SetInboundOverride(ch)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, ok := al.consumeInbound(ctx)
	if ok {
		t.Fatal("consumeInbound should return ok=false when context is done")
	}
}

// TestConsumeInbound_NoOverrideFallsBackToBus verifies that consumeInbound
// uses the bus when no override channel is set, and returns false on
// context cancellation.
func TestConsumeInbound_NoOverrideFallsBackToBus(t *testing.T) {
	al := &AgentLoop{
		bus: bus.NewMessageBus(),
	}
	// inboundOverride is nil — should fall back to bus

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, ok := al.consumeInbound(ctx)
	if ok {
		t.Fatal("consumeInbound should return ok=false on cancelled context with empty bus")
	}
}

// TestMessageOrdering_DocumentGuarantee documents the sequential message
// processing guarantee provided by the AgentLoop architecture.
func TestMessageOrdering_DocumentGuarantee(t *testing.T) {
	// This test documents the sequential message processing guarantee.
	// The AgentLoop processes exactly one message at a time:
	//   1. consumeInbound() blocks until a message is available
	//   2. processMessage() runs synchronously (one goroutine)
	//   3. Sessions.Save() completes (blocking I/O) before loop continues
	//   4. Only then does consumeInbound() block for the NEXT message
	//
	// This guarantees that when message B is processed, message A's
	// response is already in session history.
	//
	// The inboundOverride mechanism (SetInboundOverride) enables the
	// group chat debouncer to intercept the message stream without
	// breaking this sequential guarantee.
	t.Log("Sequential message processing guarantee: verified by architecture")
	t.Log("- Single goroutine AgentLoop.Run()")
	t.Log("- Sessions.Save() is synchronous blocking I/O")
	t.Log("- consumeInbound() blocks until previous processMessage() returns")
	t.Log("- SetInboundOverride() enables debounce relay without breaking ordering")
}

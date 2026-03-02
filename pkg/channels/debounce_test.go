// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package channels

import (
	"fmt"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

// drainOne reads one message from ch within timeout, failing t if none arrives.
func drainOne(t *testing.T, ch <-chan bus.InboundMessage, timeout time.Duration) (bus.InboundMessage, bool) {
	t.Helper()
	select {
	case msg, ok := <-ch:
		return msg, ok
	case <-time.After(timeout):
		return bus.InboundMessage{}, false
	}
}

// expectNoMessage asserts no message is received within timeout.
func expectNoMessage(t *testing.T, ch <-chan bus.InboundMessage, timeout time.Duration) {
	t.Helper()
	select {
	case msg := <-ch:
		t.Errorf("unexpected message received: %+v", msg)
	case <-time.After(timeout):
		// expected: no message
	}
}

// makeGroupMsg builds a group InboundMessage for testing.
func makeGroupMsg(channel, chatID, content, messageID string) bus.InboundMessage {
	return bus.InboundMessage{
		Channel:   channel,
		ChatID:    chatID,
		Content:   content,
		MessageID: messageID,
		Peer:      bus.Peer{Kind: "group", ID: chatID},
	}
}

// makeDirectMsg builds a direct InboundMessage for testing.
func makeDirectMsg(channel, chatID, content string) bus.InboundMessage {
	return bus.InboundMessage{
		Channel:   channel,
		ChatID:    chatID,
		Content:   content,
		MessageID: "dm-1",
		Peer:      bus.Peer{Kind: "direct", ID: chatID},
	}
}

// TestGroupDebouncer_DisabledPassThrough verifies that when debounce is disabled,
// all messages (group and direct) pass through immediately.
func TestGroupDebouncer_DisabledPassThrough(t *testing.T) {
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   false,
		Window:    200 * time.Millisecond,
		MaxWindow: 500 * time.Millisecond,
	})
	defer d.Close()

	msgs := []bus.InboundMessage{
		makeGroupMsg("tg", "chat1", "hello", "msg1"),
		makeDirectMsg("tg", "dm1", "direct message"),
		makeGroupMsg("tg", "chat2", "world", "msg3"),
	}

	for _, m := range msgs {
		d.HandleMessage(m)
	}

	ch := d.FlushChan()
	received := make(map[string]bool)
	for i := 0; i < len(msgs); i++ {
		msg, ok := drainOne(t, ch, 50*time.Millisecond)
		if !ok {
			t.Fatalf("expected message %d, got timeout", i)
		}
		received[msg.MessageID] = true
	}

	for _, m := range msgs {
		if !received[m.MessageID] {
			t.Errorf("message %q not received", m.MessageID)
		}
	}
}

// TestGroupDebouncer_DirectPassThrough verifies that direct messages are never buffered,
// even when debounce is enabled.
func TestGroupDebouncer_DirectPassThrough(t *testing.T) {
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   true,
		Window:    200 * time.Millisecond,
		MaxWindow: 500 * time.Millisecond,
	})
	defer d.Close()

	msg := makeDirectMsg("tg", "dm1", "direct content")
	d.HandleMessage(msg)

	ch := d.FlushChan()
	got, ok := drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("direct message should pass through immediately, got timeout")
	}
	if got.Content != msg.Content {
		t.Errorf("content mismatch: got %q, want %q", got.Content, msg.Content)
	}
}

// TestGroupDebouncer_GroupBuffered verifies that group messages are held until
// the Window elapses after the last message.
func TestGroupDebouncer_GroupBuffered(t *testing.T) {
	window := 30 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   true,
		Window:    window,
		MaxWindow: 200 * time.Millisecond,
	})
	defer d.Close()

	msg := makeGroupMsg("tg", "chat1", "buffered", "msg1")
	d.HandleMessage(msg)

	ch := d.FlushChan()

	// Should NOT arrive immediately.
	expectNoMessage(t, ch, 10*time.Millisecond)

	// After Window, it should arrive.
	got, ok := drainOne(t, ch, 100*time.Millisecond)
	if !ok {
		t.Fatal("group message should be flushed after Window, got timeout")
	}
	if got.Content != msg.Content {
		t.Errorf("content mismatch: got %q, want %q", got.Content, msg.Content)
	}
}

// TestGroupDebouncer_GroupMentionFlushImmediate verifies that @mentioned group
// messages skip debouncing and are delivered immediately.
func TestGroupDebouncer_GroupMentionFlushImmediate(t *testing.T) {
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   true,
		Window:    500 * time.Millisecond, // long window — should not fire in test
		MaxWindow: 2 * time.Second,
	})
	defer d.Close()

	msg := makeGroupMsg("tg", "chat1", "hey @bot", "msg1")
	msg.Metadata = map[string]string{"is_mentioned": "true"}

	d.HandleMessage(msg)

	ch := d.FlushChan()
	got, ok := drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("mentioned message should flush immediately, got timeout")
	}
	if got.Content != msg.Content {
		t.Errorf("content mismatch: got %q, want %q", got.Content, msg.Content)
	}
}

// TestGroupDebouncer_LastMessageFlushed verifies that when multiple messages arrive
// for the same channel+chat, only the LAST one is flushed.
func TestGroupDebouncer_LastMessageFlushed(t *testing.T) {
	window := 40 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   true,
		Window:    window,
		MaxWindow: 500 * time.Millisecond,
	})
	defer d.Close()

	msgs := []bus.InboundMessage{
		makeGroupMsg("tg", "chat1", "first", "id1"),
		makeGroupMsg("tg", "chat1", "second", "id2"),
		makeGroupMsg("tg", "chat1", "third", "id3"),
	}
	for _, m := range msgs {
		d.HandleMessage(m)
	}

	ch := d.FlushChan()

	// Wait for the window to fire.
	got, ok := drainOne(t, ch, 200*time.Millisecond)
	if !ok {
		t.Fatal("expected flushed message after Window, got timeout")
	}
	if got.MessageID != "id3" {
		t.Errorf("expected last message (id3), got %q", got.MessageID)
	}
	if got.Content != "third" {
		t.Errorf("expected content %q, got %q", "third", got.Content)
	}

	// No more messages should arrive.
	expectNoMessage(t, ch, 20*time.Millisecond)
}

// TestGroupDebouncer_MaxWindowFlush verifies that when MaxWindow is shorter than
// Window, the entry is flushed at MaxWindow even if messages keep arriving.
func TestGroupDebouncer_MaxWindowFlush(t *testing.T) {
	maxWindow := 50 * time.Millisecond
	window := 100 * time.Millisecond // longer than maxWindow

	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   true,
		Window:    window,
		MaxWindow: maxWindow,
	})
	defer d.Close()

	ch := d.FlushChan()

	// First message — starts MaxWindow timer.
	d.HandleMessage(makeGroupMsg("tg", "chat1", "first", "id1"))

	// At ~30ms, send another message (resets Window timer, but MaxWindow already running).
	time.Sleep(30 * time.Millisecond)
	d.HandleMessage(makeGroupMsg("tg", "chat1", "second", "id2"))

	// At ~60ms the MaxWindow (50ms) should have fired, before Window (100ms from now) would.
	got, ok := drainOne(t, ch, 100*time.Millisecond)
	if !ok {
		t.Fatal("expected flush at MaxWindow, got timeout")
	}
	// The last message at the time of flush should be "second".
	if got.MessageID != "id2" {
		t.Errorf("expected last message id2, got %q", got.MessageID)
	}
}

// TestGroupDebouncer_Close verifies that Close() closes flushChan.
func TestGroupDebouncer_Close(t *testing.T) {
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   true,
		Window:    50 * time.Millisecond,
		MaxWindow: 200 * time.Millisecond,
	})

	ch := d.FlushChan()
	d.Close()

	// After Close, the channel should be closed (ok == false on read).
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected FlushChan to be closed (ok=false)")
		}
		// ok == false: correct
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for FlushChan to be closed")
	}
}

// TestGroupDebouncer_DifferentChats verifies that messages for different chatIDs
// within the same channel are buffered independently.
func TestGroupDebouncer_DifferentChats(t *testing.T) {
	window := 30 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   true,
		Window:    window,
		MaxWindow: 500 * time.Millisecond,
	})
	defer d.Close()

	msgA := makeGroupMsg("tg", "chatA", "hello from A", "idA")
	msgB := makeGroupMsg("tg", "chatB", "hello from B", "idB")

	d.HandleMessage(msgA)
	d.HandleMessage(msgB)

	ch := d.FlushChan()

	// Both should arrive after Window, in any order.
	received := make(map[string]string) // chatID -> content
	for i := 0; i < 2; i++ {
		got, ok := drainOne(t, ch, 200*time.Millisecond)
		if !ok {
			t.Fatalf("expected message %d from flush, got timeout", i)
		}
		received[got.ChatID] = got.Content
	}

	if received["chatA"] != "hello from A" {
		t.Errorf("chatA: got %q, want %q", received["chatA"], "hello from A")
	}
	if received["chatB"] != "hello from B" {
		t.Errorf("chatB: got %q, want %q", received["chatB"], "hello from B")
	}

	// No more messages.
	expectNoMessage(t, ch, 20*time.Millisecond)
}



// TestGroupDebouncer_IncludedChannelIDs verifies that when IncludedChannelIDs is set,
// only messages from those specific chats are debounced; others pass through immediately.
func TestGroupDebouncer_IncludedChannelIDs(t *testing.T) {
	window := 100 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:            true,
		Window:             window,
		MaxWindow:          500 * time.Millisecond,
		IncludedChannelIDs: []string{"tg:includedChat"},
	})
	defer d.Close()

	ch := d.FlushChan()

	// Message from included chat should be debounced
	includedMsg := makeGroupMsg("tg", "includedChat", "buffered", "id1")
	d.HandleMessage(includedMsg)

	// Should NOT arrive immediately
	expectNoMessage(t, ch, 20*time.Millisecond)

	// Message from non-included chat should pass through immediately
	excludedMsg := makeGroupMsg("tg", "otherChat", "immediate", "id2")
	d.HandleMessage(excludedMsg)

	got, ok := drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("non-included chat message should pass through immediately, got timeout")
	}
	if got.MessageID != "id2" {
		t.Errorf("expected message id2, got %q", got.MessageID)
	}

	// Now wait for the included chat message to be flushed
	got, ok = drainOne(t, ch, 200*time.Millisecond)
	if !ok {
		t.Fatal("included chat message should be flushed after Window, got timeout")
	}
	if got.MessageID != "id1" {
		t.Errorf("expected message id1, got %q", got.MessageID)
	}
}

// TestGroupDebouncer_ExcludedChannelIDs verifies that when ExcludedChannelIDs is set,
// messages from those specific chats pass through immediately; others are debounced.
func TestGroupDebouncer_ExcludedChannelIDs(t *testing.T) {
	window := 100 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:            true,
		Window:             window,
		MaxWindow:          500 * time.Millisecond,
		ExcludedChannelIDs: []string{"tg:excludedChat"},
	})
	defer d.Close()

	ch := d.FlushChan()

	// Message from excluded chat should pass through immediately
	excludedMsg := makeGroupMsg("tg", "excludedChat", "immediate", "id1")
	d.HandleMessage(excludedMsg)

	got, ok := drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("excluded chat message should pass through immediately, got timeout")
	}
	if got.MessageID != "id1" {
		t.Errorf("expected message id1, got %q", got.MessageID)
	}

	// Message from non-excluded chat should be debounced
	includedMsg := makeGroupMsg("tg", "otherChat", "buffered", "id2")
	d.HandleMessage(includedMsg)

	// Should NOT arrive immediately
	expectNoMessage(t, ch, 20*time.Millisecond)

	// Now wait for it to be flushed
	got, ok = drainOne(t, ch, 200*time.Millisecond)
	if !ok {
		t.Fatal("non-excluded chat message should be flushed after Window, got timeout")
	}
	if got.MessageID != "id2" {
		t.Errorf("expected message id2, got %q", got.MessageID)
	}
}

// TestGroupDebouncer_MentionBypassesIncludeList verifies that @mentioned messages
// bypass debounce even if the chat is in the IncludedChannelIDs list.
func TestGroupDebouncer_MentionBypassesIncludeList(t *testing.T) {
	window := 200 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:            true,
		Window:             window,
		MaxWindow:          500 * time.Millisecond,
		IncludedChannelIDs: []string{"tg:chat1"}, // Only chat1 should be debounced
	})
	defer d.Close()

	ch := d.FlushChan()

	// First, send a regular message to chat1 (should be debounced)
	regularMsg := makeGroupMsg("tg", "chat1", "regular", "id1")
	d.HandleMessage(regularMsg)

	// Should NOT arrive immediately (debounced)
	expectNoMessage(t, ch, 20*time.Millisecond)

	// Now send an @mentioned message to the same chat (should bypass debounce)
	mentionedMsg := makeGroupMsg("tg", "chat1", "@bot hello", "id2")
	mentionedMsg.Metadata = map[string]string{"is_mentioned": "true"}
	d.HandleMessage(mentionedMsg)

	// First message to arrive should be the previously buffered message (id1)
	// because flushEntryLocked is called before sending the mentioned message
	got, ok := drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("previously buffered message should be flushed first after mention, got timeout")
	}
	if got.MessageID != "id1" {
		t.Errorf("expected buffered message id1 first, got %q", got.MessageID)
	}

	// Then the mentioned message (id2) should arrive
	got, ok = drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("@mentioned message should arrive after buffered messages are flushed, got timeout")
	}
	if got.MessageID != "id2" {
		t.Errorf("expected mentioned message id2, got %q", got.MessageID)
	}
}

// TestGroupDebouncer_MentionBypassesExcludeList verifies that @mentioned messages
// bypass debounce even if the chat is in the ExcludedChannelIDs list (though
// excluded chats already bypass debounce, this ensures mentions work consistently).
func TestGroupDebouncer_MentionBypassesExcludeList(t *testing.T) {
	window := 200 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:            true,
		Window:             window,
		MaxWindow:          500 * time.Millisecond,
		ExcludedChannelIDs: []string{"tg:excludedChat"}, // excludedChat bypasses debounce
	})
	defer d.Close()

	ch := d.FlushChan()

	// Message from excluded chat passes through immediately (no debounce)
	excludedMsg := makeGroupMsg("tg", "excludedChat", "hello", "id1")
	d.HandleMessage(excludedMsg)

	got, ok := drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("excluded chat message should pass through, got timeout")
	}
	if got.MessageID != "id1" {
		t.Errorf("expected message id1, got %q", got.MessageID)
	}

	// @mentioned message in excluded chat also passes through immediately
	mentionedMsg := makeGroupMsg("tg", "excludedChat", "@bot urgent", "id2")
	mentionedMsg.Metadata = map[string]string{"is_mentioned": "true"}
	d.HandleMessage(mentionedMsg)

	got, ok = drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("@mentioned message in excluded chat should pass through, got timeout")
	}
	if got.MessageID != "id2" {
		t.Errorf("expected message id2, got %q", got.MessageID)
	}
}


// TestGroupDebouncer_EmptyIncludeListDebouncesAll verifies that an empty included list
// (after initialization) still debounces all group messages.
func TestGroupDebouncer_EmptyIncludeListDebouncesAll(t *testing.T) {
	window := 30 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:            true,
		Window:             window,
		MaxWindow:          500 * time.Millisecond,
		IncludedChannelIDs: []string{}, // empty - should debounce all
	})
	defer d.Close()

	ch := d.FlushChan()

	// Message should be debounced (not pass through immediately)
	msg := makeGroupMsg("tg", "anyChat", "hello", "id1")
	d.HandleMessage(msg)

	// Should NOT arrive immediately
	expectNoMessage(t, ch, 10*time.Millisecond)

	// Should arrive after window
	got, ok := drainOne(t, ch, 100*time.Millisecond)
	if !ok {
		t.Fatal("group message should be flushed after Window, got timeout")
	}
	if got.MessageID != "id1" {
		t.Errorf("expected message id1, got %q", got.MessageID)
	}
}

// TestGroupDebouncer_IncludeAndExcludeBothSet verifies that when both lists are set,
// included takes precedence (whitelist mode).
func TestGroupDebouncer_IncludeAndExcludeBothSet(t *testing.T) {
	window := 30 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:            true,
		Window:             window,
		MaxWindow:          500 * time.Millisecond,
		IncludedChannelIDs: []string{"tg:chat1"}, // only chat1 should debounce
		ExcludedChannelIDs: []string{"tg:chat2"}, // chat2 in exclude, but not in include
	})
	defer d.Close()

	ch := d.FlushChan()

	// chat1 is in include list - should debounce
	msg1 := makeGroupMsg("tg", "chat1", "hello", "id1")
	d.HandleMessage(msg1)
	expectNoMessage(t, ch, 10*time.Millisecond)

	// chat2 is in exclude list but NOT in include - should pass through
	msg2 := makeGroupMsg("tg", "chat2", "world", "id2")
	d.HandleMessage(msg2)

	got, ok := drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("chat2 should pass through (not in include list), got timeout")
	}
	if got.MessageID != "id2" {
		t.Errorf("expected message id2, got %q", got.MessageID)
	}

	// chat1 should still flush after window
	got, ok = drainOne(t, ch, 100*time.Millisecond)
	if !ok {
		t.Fatal("chat1 should be flushed after Window, got timeout")
	}
	if got.MessageID != "id1" {
		t.Errorf("expected message id1, got %q", got.MessageID)
	}
}

// TestGroupDebouncer_DifferentChannelsIndependent verifies that messages from different
// channels (e.g., telegram vs discord) are tracked independently.
func TestGroupDebouncer_DifferentChannelsIndependent(t *testing.T) {
	window := 30 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   true,
		Window:    window,
		MaxWindow: 500 * time.Millisecond,
	})
	defer d.Close()

	ch := d.FlushChan()

	// Two messages in different channels, same chat ID
	msgTg := makeGroupMsg("telegram", "chat123", "hello from tg", "id-tg")
	msgDc := makeGroupMsg("discord", "chat123", "hello from dc", "id-dc")

	d.HandleMessage(msgTg)
	d.HandleMessage(msgDc)

	// Both should arrive after window (different channels = different buffers)
	received := make(map[string]string)
	for i := 0; i < 2; i++ {
		got, ok := drainOne(t, ch, 100*time.Millisecond)
		if !ok {
			t.Fatalf("expected message %d, got timeout", i)
		}
		received[got.Channel] = got.Content
	}

	if received["telegram"] != "hello from tg" {
		t.Errorf("telegram: got %q, want %q", received["telegram"], "hello from tg")
	}
	if received["discord"] != "hello from dc" {
		t.Errorf("discord: got %q, want %q", received["discord"], "hello from dc")
	}
}

// TestGroupDebouncer_MultipleMessagesInSequence tests rapid sequential messages
// to the same chat - only the last should be delivered.
func TestGroupDebouncer_MultipleMessagesInSequence(t *testing.T) {
	window := 50 * time.Millisecond
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   true,
		Window:    window,
		MaxWindow: 200 * time.Millisecond,
	})
	defer d.Close()

	ch := d.FlushChan()

	// Send 5 rapid messages
	for i := 1; i <= 5; i++ {
		msg := makeGroupMsg("tg", "chat1", fmt.Sprintf("message %d", i), fmt.Sprintf("id%d", i))
		d.HandleMessage(msg)
		time.Sleep(5 * time.Millisecond) // small delay between messages
	}

	// Wait for window to fire
	got, ok := drainOne(t, ch, 150*time.Millisecond)
	if !ok {
		t.Fatal("expected flushed message after Window, got timeout")
	}

	// Should be the LAST message
	if got.MessageID != "id5" {
		t.Errorf("expected last message (id5), got %q", got.MessageID)
	}
	if got.Content != "message 5" {
		t.Errorf("expected 'message 5', got %q", got.Content)
	}

	// No more messages should arrive
	expectNoMessage(t, ch, 20*time.Millisecond)
}

// TestGroupDebouncer_MentionFlushesExistingAndNew verifies that when a mention arrives,
// both any existing buffered messages AND the mention itself are flushed.
func TestGroupDebouncer_MentionFlushesExistingAndNew(t *testing.T) {
	window := 200 * time.Millisecond // long window
	d := NewGroupDebouncer(config.DebounceConfig{
		Enabled:   true,
		Window:    window,
		MaxWindow: 500 * time.Millisecond,
	})
	defer d.Close()

	ch := d.FlushChan()

	// Send two regular messages rapidly
	msg1 := makeGroupMsg("tg", "chat1", "first", "id1")
	msg2 := makeGroupMsg("tg", "chat1", "second", "id2")
	d.HandleMessage(msg1)
	d.HandleMessage(msg2)

	// Now send a mention - should flush both buffered AND the mention
	msgMention := makeGroupMsg("tg", "chat1", "@bot urgent", "id3")
	msgMention.Metadata = map[string]string{"is_mentioned": "true"}
	d.HandleMessage(msgMention)

	// Should get id2 (second) first (buffered, flushed by mention)
	got, ok := drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("expected first flushed message, got timeout")
	}
	if got.MessageID != "id2" {
		t.Errorf("expected id2 (second message), got %q", got.MessageID)
	}

	// Then id3 (mention)
	got, ok = drainOne(t, ch, 50*time.Millisecond)
	if !ok {
		t.Fatal("expected mention message, got timeout")
	}
	if got.MessageID != "id3" {
		t.Errorf("expected id3 (mention), got %q", got.MessageID)
	}

	// No more messages
	expectNoMessage(t, ch, 20*time.Millisecond)
}
// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package heartbeat

import (
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// --- ShouldFire tests ---

func TestShouldFire_Disabled(t *testing.T) {
	ms := NewMemorySleepService("/tmp")
	cfg := config.MemorySleepConfig{
		Enabled:   false,
		Interval:  24,
		TimeOfDay: "03:00",
	}
	now := time.Now()
	if ms.ShouldFire(cfg, now) {
		t.Error("Expected ShouldFire to return false when disabled")
	}
}

func TestShouldFire_RightHourNeverFired(t *testing.T) {
	ms := NewMemorySleepService("/tmp")
	// Use a specific time: 2026-01-15 03:00:00
	now := time.Date(2026, 1, 15, 3, 0, 0, 0, time.Local)
	cfg := config.MemorySleepConfig{
		Enabled:   true,
		Interval:  24,
		TimeOfDay: "03:00",
	}
	if !ms.ShouldFire(cfg, now) {
		t.Error("Expected ShouldFire to return true when never fired at the right hour")
	}
}

func TestShouldFire_RightHourFiredRecently(t *testing.T) {
	ms := NewMemorySleepService("/tmp")
	interval := 24
	// now is 03:00, lastFired was (interval - 1) hours ago — too soon
	now := time.Date(2026, 1, 15, 3, 0, 0, 0, time.Local)
	ms.lastFired = now.Add(-time.Duration(interval-1) * time.Hour)

	cfg := config.MemorySleepConfig{
		Enabled:   true,
		Interval:  interval,
		TimeOfDay: "03:00",
	}
	if ms.ShouldFire(cfg, now) {
		t.Error("Expected ShouldFire to return false when fired recently (interval not elapsed)")
	}
}

func TestShouldFire_RightHourFiredLongAgo(t *testing.T) {
	ms := NewMemorySleepService("/tmp")
	interval := 24
	// now is 03:00, lastFired was (interval + 1) hours ago — enough time has passed
	now := time.Date(2026, 1, 15, 3, 0, 0, 0, time.Local)
	ms.lastFired = now.Add(-time.Duration(interval+1) * time.Hour)

	cfg := config.MemorySleepConfig{
		Enabled:   true,
		Interval:  interval,
		TimeOfDay: "03:00",
	}
	if !ms.ShouldFire(cfg, now) {
		t.Error("Expected ShouldFire to return true when interval has elapsed")
	}
}

func TestShouldFire_WrongHour(t *testing.T) {
	ms := NewMemorySleepService("/tmp")
	// now is 14:00, time_of_day is 03:00
	now := time.Date(2026, 1, 15, 14, 0, 0, 0, time.Local)
	cfg := config.MemorySleepConfig{
		Enabled:   true,
		Interval:  24,
		TimeOfDay: "03:00",
	}
	if ms.ShouldFire(cfg, now) {
		t.Error("Expected ShouldFire to return false when current hour != target hour")
	}
}

func TestShouldFire_InvalidTimeOfDay(t *testing.T) {
	ms := NewMemorySleepService("/tmp")
	now := time.Now()
	cfg := config.MemorySleepConfig{
		Enabled:   true,
		Interval:  24,
		TimeOfDay: "not-valid",
	}
	if ms.ShouldFire(cfg, now) {
		t.Error("Expected ShouldFire to return false when TimeOfDay is invalid")
	}
}

// --- parseTimeOfDayHour tests ---

func TestParseTimeOfDayHour_Valid(t *testing.T) {
	tests := []struct {
		input    string
		wantHour int
	}{
		{"00:00", 0},
		{"03:30", 3},
		{"23:59", 23},
		{"12:00", 12},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			hour, err := parseTimeOfDayHour(tt.input)
			if err != nil {
				t.Errorf("parseTimeOfDayHour(%q) unexpected error: %v", tt.input, err)
			}
			if hour != tt.wantHour {
				t.Errorf("parseTimeOfDayHour(%q) = %d, want %d", tt.input, hour, tt.wantHour)
			}
		})
	}
}

func TestParseTimeOfDayHour_Invalid(t *testing.T) {
	tests := []struct {
		input string
	}{
		{""},
		{"abc:00"},
		{"25:00"},
		{"-1:00"},
		{"3"},
		{"03"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseTimeOfDayHour(tt.input)
			if err == nil {
				t.Errorf("parseTimeOfDayHour(%q) expected error, got nil", tt.input)
			}
		})
	}
}

// --- Fire tests ---

func TestFire_UpdatesLastFired(t *testing.T) {
	ms := NewMemorySleepService("/tmp")
	if !ms.lastFired.IsZero() {
		t.Fatal("Expected lastFired to be zero initially")
	}

	noopHandler := func(prompt, channel, chatID string) *tools.ToolResult {
		return &tools.ToolResult{ForLLM: "ok"}
	}

	ms.Fire(noopHandler, "test", "123")

	if ms.lastFired.IsZero() {
		t.Error("Expected lastFired to be updated after Fire")
	}
}

func TestFire_CallsHandler(t *testing.T) {
	ms := NewMemorySleepService("/tmp")

	var capturedPrompt, capturedChannel, capturedChatID string
	called := false

	handler := func(prompt, channel, chatID string) *tools.ToolResult {
		called = true
		capturedPrompt = prompt
		capturedChannel = channel
		capturedChatID = chatID
		return &tools.ToolResult{ForLLM: "ok"}
	}

	ms.Fire(handler, "test", "123")

	if !called {
		t.Fatal("Expected handler to be called")
	}
	if capturedChannel != "test" {
		t.Errorf("Expected channel %q, got %q", "test", capturedChannel)
	}
	if capturedChatID != "123" {
		t.Errorf("Expected chatID %q, got %q", "123", capturedChatID)
	}
	if capturedPrompt == "" {
		t.Error("Expected non-empty prompt")
	}
	if !strings.Contains(capturedPrompt, "read_file") {
		t.Errorf("Expected prompt to contain 'read_file', got: %s", capturedPrompt)
	}
	if !strings.Contains(capturedPrompt, "promote_to_memory") {
		t.Errorf("Expected prompt to contain 'promote_to_memory', got: %s", capturedPrompt)
	}
}

func TestFire_NilHandler(t *testing.T) {
	ms := NewMemorySleepService("/tmp")

	// Should not panic; lastFired should still be updated
	ms.Fire(nil, "chan", "chat")

	if ms.lastFired.IsZero() {
		t.Error("Expected lastFired to be updated even with nil handler")
	}
}

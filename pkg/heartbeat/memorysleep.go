// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package heartbeat

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
)

const memorySleepPrompt = `You are performing daily memory consolidation (memory sleep).

Your task:
1. Use read_file to read today's daily note (memory/YYYYMM/YYYYMMDD.md) and yesterday's daily note.
   - Today's path: memory/%s/%s.md
   - Yesterday's path: memory/%s/%s.md
2. Review both notes for stable facts, preferences, recurring patterns, or decisions.
3. Use promote_to_memory to add new entries, merge updated entries, remove outdated entries, and reorganise MEMORY.md as needed.
   Let your judgment guide what belongs in long-term memory — no fixed patterns required.
4. If nothing requires updating, do nothing.

Be thorough but concise. Do not output a table. Focus on what genuinely belongs in long-term memory.`

// MemorySleepService manages the nightly memory consolidation job.
type MemorySleepService struct {
	workspace string
	lastFired time.Time
	mu        sync.Mutex
}

// NewMemorySleepService creates a new MemorySleepService for the given workspace.
func NewMemorySleepService(workspace string) *MemorySleepService {
	return &MemorySleepService{workspace: workspace}
}

// ShouldFire returns true when memory sleep should trigger.
// Fires if enabled, the current hour matches cfg.TimeOfDay, and enough
// time has elapsed since lastFired (or it has never fired before).
func (ms *MemorySleepService) ShouldFire(cfg config.MemorySleepConfig, now time.Time) bool {
	if !cfg.Enabled {
		return false
	}

	targetHour, err := parseTimeOfDayHour(cfg.TimeOfDay)
	if err != nil {
		logger.WarnCF("heartbeat", "MemorySleep: invalid time_of_day",
			map[string]any{"value": cfg.TimeOfDay, "error": err.Error()})
		return false
	}

	currentHour := now.Local().Hour()
	if currentHour != targetHour {
		return false
	}

	ms.mu.Lock()
	last := ms.lastFired
	ms.mu.Unlock()

	if last.IsZero() {
		return true
	}

	interval := time.Duration(cfg.Interval) * time.Hour
	return now.Sub(last) >= interval
}

// Fire updates lastFired and calls the handler with the memory consolidation prompt.
func (ms *MemorySleepService) Fire(handler HeartbeatHandler, channel, chatID string) {
	ms.mu.Lock()
	ms.lastFired = time.Now()
	ms.mu.Unlock()

	if handler == nil {
		return
	}

	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)

	todayMonth := now.Format("200601")
	todayDay := now.Format("20060102")
	yestMonth := yesterday.Format("200601")
	yestDay := yesterday.Format("20060102")

	prompt := fmt.Sprintf(memorySleepPrompt, todayMonth, todayDay, yestMonth, yestDay)

	logger.InfoC("heartbeat", "Firing memory sleep consolidation")

	result := handler(prompt, channel, chatID)
	if result == nil {
		return
	}

	if result.IsError {
		logger.WarnCF("heartbeat", "Memory sleep failed",
			map[string]any{"error": result.ForLLM})
		return
	}

	logger.InfoC("heartbeat", "Memory sleep consolidation completed")
	_ = result
}

// parseTimeOfDayHour parses "HH:MM" and returns the hour as int.
func parseTimeOfDayHour(timeOfDay string) (int, error) {
	parts := strings.SplitN(timeOfDay, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected HH:MM format, got %q", timeOfDay)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("invalid hour in %q", timeOfDay)
	}
	return hour, nil
}

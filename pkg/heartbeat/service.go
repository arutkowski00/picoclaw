// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package heartbeat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/constants"
	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/sipeed/picoclaw/pkg/state"
	"github.com/sipeed/picoclaw/pkg/tools"
)

const (
	minIntervalMinutes     = 5
	defaultIntervalMinutes = 30
)

// HeartbeatHandler is the function type for handling heartbeat.
// It returns a ToolResult that can indicate async operations.
// channel and chatID are derived from the last active user channel or target_channel.
type HeartbeatHandler func(prompt, channel, chatID string) *tools.ToolResult

// Options configures optional heartbeat behavior.
type Options struct {
	// TargetChannel: when set, heartbeat sends here instead of last channel.
	// Format "platform:chat_id" e.g. "telegram:-1001234567890"
	TargetChannel string
	// PersistToSession: when true, heartbeat responses are added to target's session.
	PersistToSession bool
	// CatchupEnabled: when true and TargetChannel set, check for unaddressed messages.
	CatchupEnabled bool
	// ConsolidationEnabled: when true, every ConsolidationInterval cycles the
	// heartbeat fires a memory consolidation pass via promote_to_memory.
	ConsolidationEnabled bool
	// ConsolidationInterval: how many heartbeat cycles between consolidation runs.
	// Default 4 (~2h at 30-min interval). 0 disables consolidation.
	ConsolidationInterval int
}

// CatchupChecker returns (hasUnaddressed, sessionKey) for a channel:chatID.
// Used when catchup_enabled to detect user messages after last assistant.
type CatchupChecker func(channel, chatID string) (hasUnaddressed bool, sessionKey string)

// HeartbeatService manages periodic heartbeat checks
type HeartbeatService struct {
	workspace       string
	bus             *bus.MessageBus
	state           *state.Manager
	handler         HeartbeatHandler
	interval        time.Duration
	enabled         bool
	opts            Options
	persistCallback func(sessionKey, content string)
	catchupChecker  CatchupChecker
	mu              sync.RWMutex
	heartbeatCount  int
	stopChan        chan struct{}
}

// NewHeartbeatService creates a new heartbeat service
func NewHeartbeatService(workspace string, intervalMinutes int, enabled bool) *HeartbeatService {
	// Apply minimum interval
	if intervalMinutes < minIntervalMinutes && intervalMinutes != 0 {
		intervalMinutes = minIntervalMinutes
	}

	if intervalMinutes == 0 {
		intervalMinutes = defaultIntervalMinutes
	}

	return &HeartbeatService{
		workspace: workspace,
		interval:  time.Duration(intervalMinutes) * time.Minute,
		enabled:   enabled,
		state:     state.NewManager(workspace),
	}
}

// SetOptions sets optional heartbeat behavior (target channel, persist to session).
func (hs *HeartbeatService) SetOptions(opts Options) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.opts = opts
}

// SetPersistCallback sets the function to call when persist_to_session is enabled.
// The gateway provides this to add heartbeat responses to the target's session.
func (hs *HeartbeatService) SetPersistCallback(fn func(sessionKey, content string)) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.persistCallback = fn
}

// SetBus sets the message bus for delivering heartbeat results.
func (hs *HeartbeatService) SetBus(msgBus *bus.MessageBus) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.bus = msgBus
}

// SetHandler sets the heartbeat handler.
func (hs *HeartbeatService) SetHandler(handler HeartbeatHandler) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.handler = handler
}

// SetCatchupChecker sets the function to check for unaddressed messages.
// Used when catchup_enabled and target_channel are both set.
func (hs *HeartbeatService) SetCatchupChecker(fn CatchupChecker) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.catchupChecker = fn
}

// Start begins the heartbeat service
func (hs *HeartbeatService) Start() error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if hs.stopChan != nil {
		logger.InfoC("heartbeat", "Heartbeat service already running")
		return nil
	}

	if !hs.enabled {
		logger.InfoC("heartbeat", "Heartbeat service disabled")
		return nil
	}

	hs.stopChan = make(chan struct{})
	go hs.runLoop(hs.stopChan)

	logger.InfoCF("heartbeat", "Heartbeat service started", map[string]any{
		"interval_minutes": hs.interval.Minutes(),
	})

	return nil
}

// Stop gracefully stops the heartbeat service
func (hs *HeartbeatService) Stop() {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if hs.stopChan == nil {
		return
	}

	logger.InfoC("heartbeat", "Stopping heartbeat service")
	close(hs.stopChan)
	hs.stopChan = nil
}

// IsRunning returns whether the service is running
func (hs *HeartbeatService) IsRunning() bool {
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return hs.stopChan != nil
}

// runLoop runs the heartbeat ticker
func (hs *HeartbeatService) runLoop(stopChan chan struct{}) {
	ticker := time.NewTicker(hs.interval)
	defer ticker.Stop()

	// Run first heartbeat after initial delay
	time.AfterFunc(time.Second, func() {
		hs.executeHeartbeat()
	})

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			hs.executeHeartbeat()
		}
	}
}

// executeHeartbeat performs a single heartbeat check
func (hs *HeartbeatService) executeHeartbeat() {
	hs.mu.RLock()
	enabled := hs.enabled
	handler := hs.handler
	opts := hs.opts
	persistCallback := hs.persistCallback
	catchupChecker := hs.catchupChecker
	if !hs.enabled || hs.stopChan == nil {
		hs.mu.RUnlock()
		return
	}
	hs.mu.RUnlock()

	if !enabled {
		return
	}

	// Catch-up: check for unaddressed messages when catchup_enabled and target_channel are set
	if opts.CatchupEnabled && opts.TargetChannel != "" && catchupChecker != nil {
		hs.mu.RLock()
		msgBus := hs.bus
		hs.mu.RUnlock()
		if msgBus != nil {
			channel, chatID := hs.parseLastChannel(opts.TargetChannel)
			if channel != "" && chatID != "" && !constants.IsInternalChannel(channel) {
				hasUnaddressed, _ := catchupChecker(channel, chatID)
				if hasUnaddressed {
					peerKind := "direct"
					if channel == "telegram" && len(chatID) > 0 && chatID[0] == '-' {
						peerKind = "group"
					} else if channel == "discord" {
						peerKind = "channel"
					}
					catchupMsg := bus.InboundMessage{
						Channel:  channel,
						ChatID:   chatID,
						Peer:     bus.Peer{Kind: peerKind, ID: chatID},
						SenderID: "heartbeat",
						Content:  "[Catch-up: Please review the recent messages in this conversation and respond to anything that needs a response.]",
					}
					pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer pubCancel()
					if err := msgBus.PublishInbound(pubCtx, catchupMsg); err != nil {
						hs.logErrorf("Catch-up publish failed: %v", err)
					} else {
						hs.logInfof("Catch-up triggered for %s:%s", channel, chatID)
						logger.InfoCF("heartbeat", "Catch-up triggered", map[string]any{
							"channel": channel,
							"chat_id": chatID,
						})
					}
				}
			}
		}
	}

	// Increment heartbeat cycle counter and optionally fire consolidation.
	hs.mu.Lock()
	hs.heartbeatCount++
	count := hs.heartbeatCount
	hs.mu.Unlock()
	if opts.ConsolidationEnabled && opts.ConsolidationInterval > 0 &&
		count%opts.ConsolidationInterval == 0 {
		hs.fireConsolidation(handler)
	}

	logger.DebugC("heartbeat", "Executing heartbeat")

	prompt := hs.buildPrompt()
	if prompt == "" {
		logger.InfoC("heartbeat", "No heartbeat prompt (HEARTBEAT.md empty or missing)")
		return
	}

	if handler == nil {
		hs.logErrorf("Heartbeat handler not configured")
		return
	}

	// Resolve target: use target_channel if set, else last channel
	var channel, chatID, channelKey string
	if opts.TargetChannel != "" {
		channel, chatID = hs.parseLastChannel(opts.TargetChannel)
		channelKey = opts.TargetChannel
	} else {
		lastChannel := hs.state.GetLastChannel()
		channel, chatID = hs.parseLastChannel(lastChannel)
		channelKey = lastChannel
	}
	hs.logInfof("Resolved channel: %s, chatID: %s (from: %s)", channel, chatID, channelKey)

	result := handler(prompt, channel, chatID)

	if result == nil {
		hs.logInfof("Heartbeat handler returned nil result")
		return
	}

	// Handle different result types
	if result.IsError {
		hs.logErrorf("Heartbeat error: %s", result.ForLLM)
		return
	}

	if result.Async {
		hs.logInfof("Async task started: %s", result.ForLLM)
		logger.InfoCF("heartbeat", "Async heartbeat task started",
			map[string]any{
				"message": result.ForLLM,
			})
		return
	}

	// Check if silent
	if result.Silent {
		hs.logInfof("Heartbeat OK - silent")
		return
	}

	// Send result to user
	response := result.ForUser
	if response == "" {
		response = result.ForLLM
	}
	if response != "" {
		hs.sendResponse(response, channelKey, channel, chatID)
		// Persist to session so bot can recall heartbeat output
		if opts.PersistToSession && persistCallback != nil {
			useLastSessionKey := opts.TargetChannel == "" // last channel = we have LastSessionKey
			sessionKey := hs.resolveSessionKey(useLastSessionKey, channel, chatID)
			if sessionKey != "" {
				persistCallback(sessionKey, response)
			}
		}
	}

	hs.logInfof("Heartbeat completed: %s", result.ForLLM)
}

// resolveSessionKey returns the session key for the target (for persist_to_session).
func (hs *HeartbeatService) resolveSessionKey(useLastSessionKey bool, channel, chatID string) string {
	if useLastSessionKey {
		if k := hs.state.GetLastSessionKey(); k != "" {
			return k
		}
	}
	if channel != "" && chatID != "" {
		return routing.BuildDefaultSessionKeyForTargetChannel(channel, chatID)
	}
	return ""
}

// buildPrompt builds the heartbeat prompt from HEARTBEAT.md
func (hs *HeartbeatService) buildPrompt() string {
	heartbeatPath := filepath.Join(hs.workspace, "HEARTBEAT.md")

	data, err := os.ReadFile(heartbeatPath)
	if err != nil {
		if os.IsNotExist(err) {
			hs.createDefaultHeartbeatTemplate()
			return ""
		}
		hs.logErrorf("Error reading HEARTBEAT.md: %v", err)
		return ""
	}

	content := string(data)
	if len(content) == 0 {
		return ""
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	return fmt.Sprintf(`# Heartbeat Check

Current time: %s

You are a proactive AI assistant. This is a scheduled heartbeat check.
Review the following tasks and execute any necessary actions using available skills.
If there is nothing that requires attention, respond ONLY with: HEARTBEAT_OK

%s
`, now, content)
}

// createDefaultHeartbeatTemplate creates the default HEARTBEAT.md file
func (hs *HeartbeatService) createDefaultHeartbeatTemplate() {
	heartbeatPath := filepath.Join(hs.workspace, "HEARTBEAT.md")

	defaultContent := `# Heartbeat Check List

This file contains tasks for the heartbeat service to check periodically.

## Examples

- Check for unread messages
- Review upcoming calendar events
- Check device status (e.g., MaixCam)

## Instructions

- Execute ALL tasks listed below. Do NOT skip any task.
- For simple tasks (e.g., report current time), respond directly.
- For complex tasks that may take time, use the spawn tool to create a subagent.
- The spawn tool is async - subagent results will be sent to the user automatically.
- After spawning a subagent, CONTINUE to process remaining tasks.
- Only respond with HEARTBEAT_OK when ALL tasks are done AND nothing needs attention.

---

Add your heartbeat tasks below this line:
`

	if err := fileutil.WriteFileAtomic(heartbeatPath, []byte(defaultContent), 0o644); err != nil {
		hs.logErrorf("Failed to create default HEARTBEAT.md: %v", err)
	} else {
		hs.logInfof("Created default HEARTBEAT.md template")
	}
}

// sendResponse sends the heartbeat response to the target.
// channelKey is "platform:chat_id" (from last channel or target_channel config).
func (hs *HeartbeatService) sendResponse(response, channelKey, platform, chatID string) {
	hs.mu.RLock()
	msgBus := hs.bus
	hs.mu.RUnlock()

	if msgBus == nil {
		hs.logInfof("No message bus configured, heartbeat result not sent")
		return
	}

	if platform == "" || chatID == "" {
		if channelKey != "" {
			platform, chatID = hs.parseLastChannel(channelKey)
		}
		if platform == "" || chatID == "" {
			hs.logInfof("No target channel (last channel empty or target_channel invalid)")
			return
		}
	}

	// Skip internal channels that can't receive messages
	if constants.IsInternalChannel(platform) {
		hs.logInfof("Skipping internal channel: %s", platform)
		return
	}

	pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pubCancel()
	msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
		Channel: platform,
		ChatID:  chatID,
		Content: response,
	})

	hs.logInfof("Heartbeat result sent to %s", platform)
}

// parseLastChannel parses the last channel string into platform and userID.
// Returns empty strings for invalid or internal channels.
func (hs *HeartbeatService) parseLastChannel(lastChannel string) (platform, userID string) {
	if lastChannel == "" {
		return "", ""
	}

	// Parse channel format: "platform:user_id" (e.g., "telegram:123456")
	parts := strings.SplitN(lastChannel, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		hs.logErrorf("Invalid last channel format: %s", lastChannel)
		return "", ""
	}

	platform, userID = parts[0], parts[1]

	// Skip internal channels
	if constants.IsInternalChannel(platform) {
		hs.logInfof("Skipping internal channel: %s", platform)
		return "", ""
	}

	return platform, userID
}

// logInfof logs an informational message to the heartbeat log
func (hs *HeartbeatService) logInfof(format string, args ...any) {
	hs.logf("INFO", format, args...)
}

// logErrorf logs an error message to the heartbeat log
func (hs *HeartbeatService) logErrorf(format string, args ...any) {
	hs.logf("ERROR", format, args...)
}

// logf writes a message to the heartbeat log file
func (hs *HeartbeatService) logf(level, format string, args ...any) {
	logFile := filepath.Join(hs.workspace, "heartbeat.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(f, "[%s] [%s] %s\n", timestamp, level, fmt.Sprintf(format, args...))
}

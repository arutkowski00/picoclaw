// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package heartbeat

import (
	"github.com/sipeed/picoclaw/pkg/logger"
)

const consolidationPrompt = `You are performing periodic memory consolidation.

Review the recent daily notes (last 3 days) in your memory context.
For each piece of information that is a stable fact, preference, or decision
(not transient) and not already present in MEMORY.md, use promote_to_memory
to save it under an appropriate section.

Rules:
- Summarise and deduplicate — do not copy entries verbatim.
- Skip transient observations (e.g. "user asked about X today").
- Skip anything already present in MEMORY.md.
- If nothing needs promotion, do nothing.`

// fireConsolidation runs a memory consolidation pass through the heartbeat handler.
// It uses the handler with an empty channel/chatID so the result is silent (internal).
func (hs *HeartbeatService) fireConsolidation(handler HeartbeatHandler) {
	if handler == nil {
		return
	}

	logger.InfoC("heartbeat", "Firing memory consolidation pass")

	result := handler(consolidationPrompt, "cli", "direct")
	if result == nil {
		return
	}

	if result.IsError {
		logger.WarnCF("heartbeat", "Memory consolidation failed",
			map[string]any{"error": result.ForLLM})
		hs.logErrorf("Memory consolidation failed: %s", result.ForLLM)
		return
	}

	hs.logInfof("Memory consolidation completed")
	logger.InfoC("heartbeat", "Memory consolidation completed")

	// Discard result — consolidation is purely internal; promote_to_memory
	// writes directly to MEMORY.md and needs no user-facing response.
	_ = result
}


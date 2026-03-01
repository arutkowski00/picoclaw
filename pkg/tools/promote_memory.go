// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/utils"
)

// PromoteToMemoryTool writes curated information to the permanent long-term
// memory file (memory/MEMORY.md) under an optional section heading.
// This is the complement to StoreMemoryTool: store_memory is for quick
// transient capture in today's daily note; promote_to_memory is for
// facts that should persist indefinitely.
type PromoteToMemoryTool struct {
	workspace string
}

// NewPromoteToMemoryTool creates a promote_to_memory tool for the given workspace.
func NewPromoteToMemoryTool(workspace string) *PromoteToMemoryTool {
	return &PromoteToMemoryTool{workspace: workspace}
}

func (t *PromoteToMemoryTool) Name() string {
	return "promote_to_memory"
}

func (t *PromoteToMemoryTool) Description() string {
	return "Promote important information to permanent long-term memory (MEMORY.md). " +
		"Use for stable facts, verified preferences, decisions, and knowledge that should " +
		"persist indefinitely — not just for today. Optionally specify a section to organise " +
		"entries (e.g. Preferences, User Information, Important Notes). " +
		"For quick short-term capture use store_memory instead."
}

func (t *PromoteToMemoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The information to store permanently (concise, actionable)",
			},
			"section": map[string]any{
				"type":        "string",
				"description": "Optional section name in MEMORY.md (e.g. Preferences, Important Notes, User Information)",
			},
		},
		"required": []string{"content"},
	}
}

func (t *PromoteToMemoryTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	content, ok := args["content"].(string)
	if !ok || content == "" {
		return ErrorResult("content is required")
	}
	section, _ := args["section"].(string)
	section = strings.TrimSpace(section)

	memoryDir := filepath.Join(t.workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create memory dir: %v", err))
	}
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")

	var existing string
	if data, err := os.ReadFile(memoryFile); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("failed to read memory: %v", err))
	}

	now := time.Now().Format("2006-01-02")
	entry := fmt.Sprintf("- [%s] %s", now, strings.TrimSpace(content))

	var newContent string
	if section != "" {
		sectionHeader := "## " + section
		if idx := strings.Index(existing, sectionHeader); idx >= 0 {
			// Insert before the end of the existing section.
			afterHeader := idx + len(sectionHeader)
			endOfSection := len(existing)
			if nextSection := strings.Index(existing[afterHeader:], "\n## "); nextSection >= 0 {
				endOfSection = afterHeader + nextSection
			}
			insertAt := endOfSection
			for insertAt > 0 && (existing[insertAt-1] == '\n' || existing[insertAt-1] == ' ') {
				insertAt--
			}
			newContent = existing[:insertAt] + "\n" + entry + "\n" + existing[insertAt:]
		} else {
			// Section doesn't exist — append a new one.
			newContent = strings.TrimRight(existing, "\n") + "\n\n" + sectionHeader + "\n\n" + entry + "\n"
		}
	} else {
		newContent = strings.TrimRight(existing, "\n") + "\n" + entry + "\n"
	}

	if err := fileutil.WriteFileAtomic(memoryFile, []byte(newContent), 0o600); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write memory: %v", err))
	}
	return SilentResult(fmt.Sprintf("Promoted to long-term memory: %s", utils.Truncate(content, 60)))
}

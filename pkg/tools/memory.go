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
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/utils"
)

// dailyNoteMu serialises concurrent read-modify-write operations on daily note
// files. Atomic rename protects readers from torn writes but does not prevent
// two goroutines from clobbering each other's appends; a single in-process
// mutex is sufficient because picoclaw is a single-process binary.
var dailyNoteMu sync.Mutex

// StoreMemoryTool appends important information to today's daily note
// (memory/YYYYMM/YYYYMMDD.md). For permanent long-term storage use
// promote_to_memory instead.
type StoreMemoryTool struct {
	workspace string
}

// NewStoreMemoryTool creates a store_memory tool for the given workspace.
func NewStoreMemoryTool(workspace string) *StoreMemoryTool {
	return &StoreMemoryTool{workspace: workspace}
}

func (t *StoreMemoryTool) Name() string {
	return "store_memory"
}

func (t *StoreMemoryTool) Description() string {
	return "Capture important information in today's daily note for future reference. " +
		"Call this proactively when you learn something the user might want you to recall — " +
		"preferences, facts, decisions, IDs, booking refs. Do not wait for the user to ask. " +
		"For permanent long-term memory that should survive indefinitely, use promote_to_memory."
}

func (t *StoreMemoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The information to store (concise, actionable)",
			},
		},
		"required": []string{"content"},
	}
}

func (t *StoreMemoryTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	content, ok := args["content"].(string)
	if !ok || content == "" {
		return ErrorResult("content is required")
	}

	// Compute today's daily note path: memory/YYYYMM/YYYYMMDD.md
	memoryDir := filepath.Join(t.workspace, "memory")
	today := time.Now().Format("20060102")
	monthDir := today[:6]
	noteDir := filepath.Join(memoryDir, monthDir)
	if err := os.MkdirAll(noteDir, 0o755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create directory: %v", err))
	}
	todayFile := filepath.Join(noteDir, today+".md")

	// Serialise concurrent appends to the same file.
	dailyNoteMu.Lock()
	defer dailyNoteMu.Unlock()

	var existing string
	if data, err := os.ReadFile(todayFile); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("failed to read daily note: %v", err))
	}

	now := time.Now().Format("2006-01-02 15:04")
	entry := fmt.Sprintf("- [%s] %s", now, strings.TrimSpace(content))

	var newContent string
	if existing == "" {
		header := fmt.Sprintf("# %s\n\n", time.Now().Format("2006-01-02"))
		newContent = header + entry + "\n"
	} else {
		newContent = strings.TrimRight(existing, "\n") + "\n" + entry + "\n"
	}

	if err := fileutil.WriteFileAtomic(todayFile, []byte(newContent), 0o600); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write daily note: %v", err))
	}
	return SilentResult(fmt.Sprintf("Stored: %s", utils.Truncate(content, 60)))
}

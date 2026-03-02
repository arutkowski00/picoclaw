package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDebounceConfigUnmarshalJSON(t *testing.T) {
	jsonStr := `{
		"enabled": true,
		"window": "30s",
		"max_window": "60s",
		"included_channel_ids": ["telegram:123"]
	}`

	var cfg DebounceConfig
	err := json.Unmarshal([]byte(jsonStr), &cfg)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if !cfg.Enabled {
		t.Error("Expected Enabled=true")
	}
	if cfg.Window != 30*time.Second {
		t.Errorf("Expected Window=30s, got %v", cfg.Window)
	}
	if cfg.MaxWindow != 60*time.Second {
		t.Errorf("Expected MaxWindow=60s, got %v", cfg.MaxWindow)
	}
	if len(cfg.IncludedChannelIDs) != 1 || cfg.IncludedChannelIDs[0] != "telegram:123" {
		t.Errorf("Expected IncludedChannelIDs=[telegram:123], got %v", cfg.IncludedChannelIDs)
	}
}

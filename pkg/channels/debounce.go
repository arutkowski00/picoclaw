// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package channels

import (
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

// debounceEntry holds buffered messages and timers for a single group chat key.
type debounceEntry struct {
	msgs      []bus.InboundMessage
	timer     *time.Timer // fires after Window of inactivity
	maxTimer  *time.Timer // fires after MaxWindow from first message
	key       string
}

// GroupDebouncer delays processing of group chat messages by a configurable
// time window, batching rapid-fire messages into one. Private chats and
// @mentions always pass through immediately.
type GroupDebouncer struct {
	cfg       config.DebounceConfig
	entries   map[string]*debounceEntry
	flushChan chan bus.InboundMessage
	mu        sync.Mutex
	stopCh    chan struct{}
}

// NewGroupDebouncer creates a new GroupDebouncer with the given configuration.
func NewGroupDebouncer(cfg config.DebounceConfig) *GroupDebouncer {
	return &GroupDebouncer{
		cfg:       cfg,
		entries:   make(map[string]*debounceEntry),
		flushChan: make(chan bus.InboundMessage, 64),
		stopCh:    make(chan struct{}),
	}
}

// FlushChan returns the read-only channel of debounced (or pass-through) messages.
func (d *GroupDebouncer) FlushChan() <-chan bus.InboundMessage {
	return d.flushChan
}

// shouldDebounceForChat checks if debounce should apply to this specific channel:chatID.
// Logic:
//   - If IncludedChannelIDs is non-empty, only debounce if the key is in the list.
//   - If ExcludedChannelIDs is non-empty, skip debounce if the key is in the list.
//   - Otherwise, debounce normally.
func (d *GroupDebouncer) shouldDebounceForChat(channel, chatID string) bool {
	key := channel + ":" + chatID

	// Check included list first (whitelist mode)
	if len(d.cfg.IncludedChannelIDs) > 0 {
		for _, id := range d.cfg.IncludedChannelIDs {
			if id == key {
				return true
			}
		}
		return false // not in include list, don't debounce
	}

	// Check excluded list (blacklist mode)
	if len(d.cfg.ExcludedChannelIDs) > 0 {
		for _, id := range d.cfg.ExcludedChannelIDs {
			if id == key {
				return false // in exclude list, don't debounce
			}
		}
	}

	return true
}

// HandleMessage processes a single inbound message through the debouncer.
// - If debounce is disabled OR the peer is not a group: write directly to flushChan.
// - If the chat is not in IncludedChannelIDs (when specified): write directly to flushChan.
// - If the chat is in ExcludedChannelIDs: write directly to flushChan.
// - If the message is @mentioned: flush the buffered entry immediately.
// - Otherwise: buffer the message and reset the Window timer.
func (d *GroupDebouncer) HandleMessage(msg bus.InboundMessage) {
	// Pass-through: debounce disabled or not a group message
	if !d.cfg.Enabled || msg.Peer.Kind != "group" {
		select {
		case d.flushChan <- msg:
		case <-d.stopCh:
		}
		return
	}

	// Pass-through: chat not in include list or in exclude list
	if !d.shouldDebounceForChat(msg.Channel, msg.ChatID) {
		select {
		case d.flushChan <- msg:
		case <-d.stopCh:
		}
		return
	}

	// Pass-through: @mentioned messages flush immediately
	if msg.Metadata["is_mentioned"] == "true" {
		key := msg.Channel + ":" + msg.ChatID
		d.mu.Lock()
		if entry, ok := d.entries[key]; ok {
			// Cancel pending timers and flush buffered messages first, then flush this one
			d.flushEntryLocked(key, entry)
		}
		d.mu.Unlock()
		// Send the mention message directly, without buffering
		select {
		case d.flushChan <- msg:
		case <-d.stopCh:
		}
		return
	}
	// Pass-through: debounce disabled or not a group message
	if !d.cfg.Enabled || msg.Peer.Kind != "group" {
		select {
		case d.flushChan <- msg:
		case <-d.stopCh:
		}
		return
	}

	// Pass-through: chat not in include list or in exclude list
	if !d.shouldDebounceForChat(msg.Channel, msg.ChatID) {
		select {
		case d.flushChan <- msg:
		case <-d.stopCh:
		}
		return
	}

	// Pass-through: @mentioned messages flush immediately
	if msg.Metadata["is_mentioned"] == "true" {
		key := msg.Channel + ":" + msg.ChatID
		d.mu.Lock()
		if entry, ok := d.entries[key]; ok {
			// Cancel pending timers and flush buffered messages first, then flush this one
			d.flushEntryLocked(key, entry)
		}
		d.mu.Unlock()
		// Send the mention message directly, without buffering
		select {
		case d.flushChan <- msg:
		case <-d.stopCh:
		}
		return
	}

	// Buffer the message
	key := msg.Channel + ":" + msg.ChatID
	d.mu.Lock()
	entry, ok := d.entries[key]
	if !ok {
		entry = &debounceEntry{key: key}
		d.entries[key] = entry
	}
	entry.msgs = append(entry.msgs, msg)

	// Reset the Window timer (debounce: fires after inactivity)
	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.timer = time.AfterFunc(d.cfg.Window, func() {
		d.mu.Lock()
		e, exists := d.entries[key]
		if exists {
			d.flushEntryLocked(key, e)
		}
		d.mu.Unlock()
	})

	// Start MaxWindow timer only on first message in this batch
	if entry.maxTimer == nil {
		entry.maxTimer = time.AfterFunc(d.cfg.MaxWindow, func() {
			d.mu.Lock()
			e, exists := d.entries[key]
			if exists {
				d.flushEntryLocked(key, e)
			}
			d.mu.Unlock()
		})
	}
	d.mu.Unlock()
}

// flushEntryLocked flushes the LAST message in the entry's buffer to flushChan,
// cancels both timers, and removes the entry. Must be called with d.mu held.
func (d *GroupDebouncer) flushEntryLocked(key string, entry *debounceEntry) {
	// Cancel both timers
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	if entry.maxTimer != nil {
		entry.maxTimer.Stop()
		entry.maxTimer = nil
	}
	// Remove entry from map
	delete(d.entries, key)

	// Flush the most recent (last) message in the batch
	if len(entry.msgs) == 0 {
		return
	}
	last := entry.msgs[len(entry.msgs)-1]
	// Non-blocking send — if flushChan is full, drop (prevent deadlock under mutex)
	select {
	case d.flushChan <- last:
	case <-d.stopCh:
	default:
		// flushChan full; send without holding mutex by launching goroutine
		go func() {
			select {
			case d.flushChan <- last:
			case <-d.stopCh:
			}
		}()
	}
}

// Close signals the debouncer to stop, and closes flushChan.
func (d *GroupDebouncer) Close() {
	select {
	case <-d.stopCh:
		// already closed
	default:
		close(d.stopCh)
	}
	close(d.flushChan)
}

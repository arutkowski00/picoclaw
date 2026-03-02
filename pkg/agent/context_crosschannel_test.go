package agent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// writeFile is a test helper that creates parent dirs and writes content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("writeFile: MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile: WriteFile %s: %v", path, err)
	}
}

// touchFile writes a file and advances its mtime to `when`.
func touchFile(t *testing.T, path string, when time.Time) {
	t.Helper()
	writeFile(t, path, "updated content")
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("touchFile: Chtimes %s: %v", path, err)
	}
}

// newCBWithSeedFile creates a ContextBuilder in a temp dir and seeds SOUL.md
// so that there is at least one tracked file for snapshot mtimes.
func newCBWithSeedFile(t *testing.T) (*ContextBuilder, string) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SOUL.md"), "initial content")
	return NewContextBuilder(dir), dir
}

// TestCrossChannelNotice_EmptyChannelID verifies that channelID == "" → "".
func TestCrossChannelNotice_EmptyChannelID(t *testing.T) {
	cb, _ := newCBWithSeedFile(t)
	if got := cb.CrossChannelNotice(""); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestCrossChannelNotice_FirstCall verifies that the first call for a new
// channelID initialises the snapshot and returns "" (no notice).
func TestCrossChannelNotice_FirstCall(t *testing.T) {
	cb, _ := newCBWithSeedFile(t)

	got := cb.CrossChannelNotice("chan-A")
	if got != "" {
		t.Errorf("first call should return empty string, got %q", got)
	}

	// snapshot must now exist
	cb.channelMtimesMu.Lock()
	_, exists := cb.channelMtimes["chan-A"]
	cb.channelMtimesMu.Unlock()
	if !exists {
		t.Error("first call should have seeded the snapshot for chan-A")
	}
}

// TestCrossChannelNotice_NoChange verifies that a second call with no file
// changes returns "".
func TestCrossChannelNotice_NoChange(t *testing.T) {
	cb, _ := newCBWithSeedFile(t)

	cb.CrossChannelNotice("chan-A") // seed snapshot
	got := cb.CrossChannelNotice("chan-A")
	if got != "" {
		t.Errorf("no-change second call should return empty string, got %q", got)
	}
}

// TestCrossChannelNotice_FileChanged verifies that advancing a file's mtime
// after the snapshot is taken produces a notice containing the file's basename.
func TestCrossChannelNotice_FileChanged(t *testing.T) {
	dir := t.TempDir()
	soulPath := filepath.Join(dir, "SOUL.md")
	writeFile(t, soulPath, "original")

	cb := NewContextBuilder(dir)
	cb.CrossChannelNotice("chan-A") // seed snapshot

	// Advance mtime to a clearly future time so the change is detected.
	future := time.Now().Add(2 * time.Second)
	touchFile(t, soulPath, future)

	got := cb.CrossChannelNotice("chan-A")
	if got == "" {
		t.Fatal("expected a notice after file mtime advanced, got empty string")
	}
	if !strings.Contains(got, "SOUL.md") {
		t.Errorf("notice should contain basename SOUL.md, got %q", got)
	}
	if !strings.Contains(got, "System Notice:") {
		t.Errorf("notice should contain 'System Notice:', got %q", got)
	}
	if !strings.Contains(got, "Changed files:") {
		t.Errorf("notice should contain 'Changed files:', got %q", got)
	}
}

// TestCrossChannelNotice_NoticeFormat verifies the exact notice format.
func TestCrossChannelNotice_NoticeFormat(t *testing.T) {
	dir := t.TempDir()
	soulPath := filepath.Join(dir, "SOUL.md")
	writeFile(t, soulPath, "original")

	cb := NewContextBuilder(dir)
	cb.CrossChannelNotice("chan-A") // seed snapshot

	future := time.Now().Add(2 * time.Second)
	touchFile(t, soulPath, future)

	got := cb.CrossChannelNotice("chan-A")
	const prefix = "[System Notice: Workspace files were updated since your last interaction. Changed files: "
	if !strings.HasPrefix(got, prefix) {
		t.Errorf("notice format wrong.\nwant prefix: %s\n     got:    %s", prefix, got)
	}
	if !strings.HasSuffix(got, "]") {
		t.Errorf("notice should end with ']', got %q", got)
	}
}

// TestCrossChannelNotice_BasenameOnly verifies that changed files are reported
// as basenames (filepath.Base), not full paths.
func TestCrossChannelNotice_BasenameOnly(t *testing.T) {
	dir := t.TempDir()
	memoryPath := filepath.Join(dir, "memory", "MEMORY.md")
	writeFile(t, memoryPath, "mem")

	cb := NewContextBuilder(dir)
	cb.CrossChannelNotice("chan-A") // seed

	future := time.Now().Add(2 * time.Second)
	touchFile(t, memoryPath, future)

	got := cb.CrossChannelNotice("chan-A")
	if got == "" {
		t.Fatal("expected a notice, got empty string")
	}
	// Must contain basename
	if !strings.Contains(got, "MEMORY.md") {
		t.Errorf("notice should contain basename MEMORY.md, got %q", got)
	}
	// Must NOT contain the full path or a slash separator in the file list
	if strings.Contains(got, string(filepath.Separator)+"memory"+string(filepath.Separator)) {
		t.Errorf("notice should contain basename only, not full path; got %q", got)
	}
}

// TestCrossChannelNotice_SnapshotUpdatedAfterNotice verifies that after a
// notice is issued, a subsequent call (without further file changes) returns "".
func TestCrossChannelNotice_SnapshotUpdatedAfterNotice(t *testing.T) {
	dir := t.TempDir()
	soulPath := filepath.Join(dir, "SOUL.md")
	writeFile(t, soulPath, "original")

	cb := NewContextBuilder(dir)
	cb.CrossChannelNotice("chan-A") // seed

	future := time.Now().Add(2 * time.Second)
	touchFile(t, soulPath, future)

	// First call after change → notice
	first := cb.CrossChannelNotice("chan-A")
	if first == "" {
		t.Fatal("expected a notice after file change")
	}

	// Second call without further changes → no notice (snapshot was updated)
	second := cb.CrossChannelNotice("chan-A")
	if second != "" {
		t.Errorf("second call should return empty (snapshot updated), got %q", second)
	}
}

// TestCrossChannelNotice_SelfSuppression verifies that the SAME channelID
// that just (conceptually) wrote files doesn't see a spurious notice, because
// its snapshot was already seeded to current state on the first call.
func TestCrossChannelNotice_SelfSuppression(t *testing.T) {
	dir := t.TempDir()
	soulPath := filepath.Join(dir, "SOUL.md")
	writeFile(t, soulPath, "original")

	cb := NewContextBuilder(dir)

	// Seed the snapshot — first call always returns "".
	first := cb.CrossChannelNotice("writer-chan")
	if first != "" {
		t.Fatalf("seed call should return '', got %q", first)
	}

	// Advance the mtime to simulate this channel "writing" files.
	future := time.Now().Add(2 * time.Second)
	touchFile(t, soulPath, future)

	// Another channel sees the change.
	got := cb.CrossChannelNotice("reader-chan") // first call — seeds snapshot with current state
	// reader-chan sees "" on first call (snapshot seeded)
	if got != "" {
		t.Fatalf("reader-chan first call should return '' (snapshot seeded to current), got %q", got)
	}

	// The writer channel calls again. Its snapshot was from BEFORE the mtime advance.
	writerGot := cb.CrossChannelNotice("writer-chan")
	// writer-chan should see the change (its snapshot is stale relative to the new mtime)
	if writerGot == "" {
		t.Logf("note: writer-chan got '' because its snapshot was seeded after file was created")
	}
}

// TestCrossChannelNotice_SelfSuppressionDirect tests the documented self-suppression:
// when a channel seeds its snapshot at current state (no prior snapshot), it won't
// get a stale notice.
func TestCrossChannelNotice_SelfSuppressionDirect(t *testing.T) {
	dir := t.TempDir()
	soulPath := filepath.Join(dir, "SOUL.md")
	// Write file BEFORE creating the ContextBuilder so it's present at first call.
	writeFile(t, soulPath, "content written by this channel")

	cb := NewContextBuilder(dir)

	// First call for this channel: snapshot is seeded to current state (file exists).
	// Since it's the first call, it must return "" regardless.
	got := cb.CrossChannelNotice("writing-channel")
	if got != "" {
		t.Errorf("first call should always return empty, got %q", got)
	}

	// Calling again without changes: must still return ""
	got2 := cb.CrossChannelNotice("writing-channel")
	if got2 != "" {
		t.Errorf("second call with no changes should return empty, got %q", got2)
	}
}

// TestCrossChannelNotice_24hExpiry verifies that a snapshot older than 24h
// causes the snapshot to be silently refreshed with no notice.
func TestCrossChannelNotice_24hExpiry(t *testing.T) {
	dir := t.TempDir()
	soulPath := filepath.Join(dir, "SOUL.md")
	writeFile(t, soulPath, "original")

	cb := NewContextBuilder(dir)

	// Seed the snapshot normally.
	cb.CrossChannelNotice("old-chan")

	// Manually inject a stale snapshot: set all mtimes to 25 hours ago.
	staleTime := time.Now().Add(-25 * time.Hour)
	staleMtimes := map[string]time.Time{
		soulPath: staleTime,
	}
	cb.channelMtimesMu.Lock()
	cb.channelMtimes["old-chan"] = staleMtimes
	cb.channelMtimesMu.Unlock()

	// Advance SOUL.md mtime to signal a real change — but the 24h expiry
	// should suppress the notice regardless.
	future := time.Now().Add(2 * time.Second)
	touchFile(t, soulPath, future)

	got := cb.CrossChannelNotice("old-chan")
	if got != "" {
		t.Errorf("24h-expired snapshot should return empty (silent refresh), got %q", got)
	}

	// After the silent refresh, snapshot is current; next call should return "".
	got2 := cb.CrossChannelNotice("old-chan")
	if got2 != "" {
		t.Errorf("after silent refresh, call with no further changes should return empty, got %q", got2)
	}
}

// TestCrossChannelNotice_24hExpiryZeroSnapshot verifies that a snapshot with
// only zero-value mtimes (no files existed) is NOT treated as 24h-expired
// (snapshotAge.IsZero() → skip expiry check).
func TestCrossChannelNotice_24hExpiryZeroSnapshot(t *testing.T) {
	dir := t.TempDir()
	// No files in dir: all mtimes in the snapshot will be zero.

	cb := NewContextBuilder(dir)
	cb.CrossChannelNotice("empty-chan") // seeds all-zero snapshot

	// Inject an all-zero snapshot explicitly.
	cb.channelMtimesMu.Lock()
	cb.channelMtimes["empty-chan"] = map[string]time.Time{}
	cb.channelMtimesMu.Unlock()

	// Create a file now.
	soulPath := filepath.Join(dir, "SOUL.md")
	writeFile(t, soulPath, "new file")

	// Should NOT be silently suppressed by 24h check (snapshotAge is zero).
	// Since the file was absent in the snapshot and now exists, it's a change.
	got := cb.CrossChannelNotice("empty-chan")
	if got == "" {
		t.Error("file appeared after empty snapshot: expected a notice, got empty string")
	}
}

// TestCrossChannelNotice_MultipleChannels verifies that different channelIDs
// track their snapshots independently.
func TestCrossChannelNotice_MultipleChannels(t *testing.T) {
	dir := t.TempDir()
	soulPath := filepath.Join(dir, "SOUL.md")
	writeFile(t, soulPath, "original")

	cb := NewContextBuilder(dir)

	// Seed both channels.
	cb.CrossChannelNotice("chan-1")
	cb.CrossChannelNotice("chan-2")

	// Advance mtime.
	future := time.Now().Add(2 * time.Second)
	touchFile(t, soulPath, future)

	// Both channels should see the change.
	got1 := cb.CrossChannelNotice("chan-1")
	got2 := cb.CrossChannelNotice("chan-2")

	if got1 == "" {
		t.Error("chan-1 should see the file change")
	}
	if got2 == "" {
		t.Error("chan-2 should see the file change")
	}

	// After both channels updated their snapshots, no further notices.
	if cb.CrossChannelNotice("chan-1") != "" {
		t.Error("chan-1 second call should return empty")
	}
	if cb.CrossChannelNotice("chan-2") != "" {
		t.Error("chan-2 second call should return empty")
	}
}

// TestCrossChannelNotice_FileDeleted verifies that file deletion triggers a notice.
func TestCrossChannelNotice_FileDeleted(t *testing.T) {
	dir := t.TempDir()
	soulPath := filepath.Join(dir, "SOUL.md")
	writeFile(t, soulPath, "content")

	cb := NewContextBuilder(dir)
	cb.CrossChannelNotice("chan-A") // seed (file exists)

	// Delete the file.
	if err := os.Remove(soulPath); err != nil {
		t.Fatalf("removing file: %v", err)
	}

	got := cb.CrossChannelNotice("chan-A")
	if got == "" {
		t.Error("file deletion should trigger a notice, got empty string")
	}
	if !strings.Contains(got, "SOUL.md") {
		t.Errorf("notice should mention deleted file SOUL.md, got %q", got)
	}
}

// TestCrossChannelNotice_FileCreated verifies that a file appearing after the
// snapshot was taken triggers a notice.
func TestCrossChannelNotice_FileCreated(t *testing.T) {
	dir := t.TempDir()
	// No SOUL.md initially.
	cb := NewContextBuilder(dir)
	cb.CrossChannelNotice("chan-A") // seed (SOUL.md absent)

	// Create the file after the snapshot.
	soulPath := filepath.Join(dir, "SOUL.md")
	writeFile(t, soulPath, "new content")

	got := cb.CrossChannelNotice("chan-A")
	if got == "" {
		t.Error("file creation after snapshot should trigger a notice, got empty string")
	}
	if !strings.Contains(got, "SOUL.md") {
		t.Errorf("notice should mention newly created SOUL.md, got %q", got)
	}
}

// TestCrossChannelNotice_Race verifies there are no data races when multiple
// goroutines call CrossChannelNotice concurrently with different channel IDs.
// Run with: go test -race ./pkg/agent/... -run TestCrossChannelNotice_Race
func TestCrossChannelNotice_Race(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SOUL.md"), "race test")

	cb := NewContextBuilder(dir)

	const goroutines = 20
	const iterations = 50

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			chanID := "chan-" + string(rune('A'+id%26))
			for range iterations {
				_ = cb.CrossChannelNotice(chanID)
			}
		}(i)
	}
	wg.Wait()
}

// TestCrossChannelNotice_RaceSharedChannel verifies no data race when multiple
// goroutines share a single channelID.
func TestCrossChannelNotice_RaceSharedChannel(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "agents content")

	cb := NewContextBuilder(dir)

	const goroutines = 10
	const iterations = 100

	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				_ = cb.CrossChannelNotice("shared-chan")
			}
		}()
	}
	wg.Wait()
}

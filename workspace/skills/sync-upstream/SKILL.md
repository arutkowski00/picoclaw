---
name: sync-upstream
description: "Sync this picoclaw fork with the upstream sipeed/picoclaw repository. Use when asked to: sync with upstream, pull upstream changes, update from upstream, merge upstream, or keep the fork up to date. Handles fetching, divergence analysis, merging, and conflict resolution for the picoclaw Go codebase."
---

# Sync Upstream Skill

Syncs this fork (`origin`) with `upstream` (https://github.com/sipeed/picoclaw).

## Quick Reference

```bash
# One-shot sync
git fetch upstream --no-tags
git merge upstream/main --no-edit --no-ff -m "chore: sync with upstream sipeed/picoclaw

<summary of upstream changes>

Co-authored-by: Sisyphus <clio-agent@sisyphuslabs.ai>"
```

## Step-by-Step

### 1. Ensure upstream remote exists

```bash
git remote -v
# If upstream is missing:
git remote add upstream https://github.com/sipeed/picoclaw.git
```

### 2. Fetch upstream

```bash
git fetch upstream --no-tags
```

### 3. Analyze divergence

```bash
MERGE_BASE=$(git merge-base main upstream/main)
echo "Upstream ahead by: $(git log --oneline $MERGE_BASE..upstream/main | wc -l) commits"
echo "We are ahead by:   $(git log --oneline $MERGE_BASE..HEAD | wc -l) commits"
git log --oneline $MERGE_BASE..upstream/main   # what upstream added
git log --oneline $MERGE_BASE..HEAD             # our unique commits
```

### 4. Merge with a descriptive commit message

```bash
git merge upstream/main --no-edit --no-ff -m "chore: sync with upstream sipeed/picoclaw

Merge N upstream commits covering:
- <bullet summary of upstream changes>

Co-authored-by: Sisyphus <clio-agent@sisyphuslabs.ai>"
```

If the merge fails due to conflicts, see **Conflict Resolution** below.

### 5. Build to verify

```bash
nix develop --command go build ./...
# or, if go is on PATH:
go build ./...
```

### 6. Commit (if there were conflicts that needed manual resolution)

```bash
git add <resolved-files>
git commit --no-edit
```

---

## Conflict Resolution

This codebase has predictable conflict zones. Resolve using the **keep-both** strategy: preserve our additions alongside upstream's changes.

### Common conflict: `pkg/agent/instance.go` (tool registration)

Upstream sometimes changes tool constructor signatures (e.g., adds `allowWritePaths` param).
We add new tool registrations (e.g., `StoreMemoryTool`, `PromoteToMemoryTool`).

**Resolution pattern** — take upstream's updated signatures AND keep our new registrations:

```go
// WRONG: pick one side
toolsRegistry.Register(tools.NewEditFileTool(workspace, restrict))          // our old signature
toolsRegistry.Register(tools.NewEditFileTool(workspace, restrict, allowWritePaths)) // upstream new

// RIGHT: upstream signature + our additions
toolsRegistry.Register(tools.NewEditFileTool(workspace, restrict, allowWritePaths))
toolsRegistry.Register(tools.NewAppendFileTool(workspace, restrict, allowWritePaths))
toolsRegistry.Register(tools.NewStoreMemoryTool(workspace))       // our addition — keep
toolsRegistry.Register(tools.NewPromoteToMemoryTool(workspace))   // our addition — keep
```

### General conflict strategy

| Conflict type | Strategy |
|---|---|
| Upstream changes function signature | Use upstream signature; keep our call-site additions |
| Upstream adds new channel/provider | Accept upstream addition; no change needed |
| Upstream edits a file we also edited | Merge both diffs manually, keeping both intents |
| Config schema changes | Take upstream schema; verify our fields still map correctly |
| README / docs | Accept upstream, re-apply our fork-specific notes if any |

After resolving each file:
```bash
git add <file>
# when all resolved:
git merge --continue   # or: git commit --no-edit
```

To abort and return to pre-merge state:
```bash
git merge --abort
```

---

## Tracking Upstream Branches

Upstream has feature branches worth monitoring:

```bash
git branch -r | grep upstream/   # list all upstream branches
git log --oneline upstream/feat/agent-browser-tool  # inspect a branch
```

To merge a specific upstream branch instead of main:
```bash
git merge upstream/<branch-name> --no-edit --no-ff
```

---

## Notes

- Always use `--no-ff` to preserve the merge topology (shows clearly in `git log --graph`)
- Always use `--no-tags` on fetch to avoid pulling upstream release tags into our remote
- The build check (`go build ./...`) is mandatory before declaring the sync done
- Our fork-specific features live in: `pkg/tools/memory.go`, `pkg/tools/promote_memory.go`, `pkg/heartbeat/`, `pkg/skills/`

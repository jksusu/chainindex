## [ERR-20260927-001] mkdir_target_directory

**Logged**: 2026-09-27T00:00:00+08:00
**Priority**: low
**Status**: resolved
**Area**: infra

### Summary
Initial sandboxed directory creation in the user-specified target directory was denied.

### Error
```
mkdir: /Users/links/Code/chainindex/docs: Operation not permitted
```

### Context
- Attempted to create `docs/superpowers/specs` in `/Users/links/Code/chainindex`.
- The directory is user-specified but requires an explicit sandbox escalation.

### Suggested Fix
Request narrowly scoped permission before creating project directories outside the active workspace root.

### Metadata
- Reproducible: yes
- Related Files: /Users/links/Code/chainindex/docs/superpowers/specs/2026-09-27-chainindex-design.md

### Resolution
- **Resolved**: 2026-09-27T00:00:00+08:00
- **Notes**: Created the directories after scoped user approval.

---

## [ERR-20260927-002] go_build_cache_target

**Logged**: 2026-09-27T00:00:00+08:00
**Priority**: low
**Status**: resolved
**Area**: tests

### Summary
The sandbox denied creation of a workspace-local Go build cache.

### Error
```
failed to initialize build cache at /Users/links/Code/chainindex/.cache/go-build: mkdir /Users/links/Code/chainindex/.cache: operation not permitted
```

### Suggested Fix
Use a task-specific cache directory under `/private/tmp`, which is writable in this environment.

### Resolution
- **Resolved**: 2026-09-27T00:00:00+08:00
- **Notes**: Subsequent Go verification uses `/private/tmp/chainindex-go-build`.

---

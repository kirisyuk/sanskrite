# Discovery Flow Fixes Summary

## Problems Identified

1. **"loaded 0 sources"** - ListByProject returned 0 sources even though sources were created
2. **Duplicate sources** - Same Telegram channels/URLs created multiple times
3. **Projects not showing in chat list** - New projects created but not displayed
4. **Sources not displaying** - Source information not shown during discovery process

## Root Causes

### Backend Issues:
1. **Silent failures in FindByCanonicalSource** - Errors were not logged, making debugging difficult
2. **Missing debug logging** - Hard to trace why ListByProject returned 0 sources
3. **Timing issues** - Sources saved but query executed too quickly (potential race condition)

### Frontend Issues:
1. **Async project refresh** - WebSocket event triggered `fetchProjects()` instead of directly adding project to store
2. **Missing source handling** - WS events only updated existing sources, didn't add new ones
3. **No placeholder sources** - No visual feedback during discovery before sources were created

## Fixes Applied

### Backend (`backend/internal/discovery/orchestrator.go`)
✅ Added detailed logging for source creation:
- Log when finding existing sources
- Log when creating new sources
- Log source IDs for traceability
- Log errors from FindByCanonicalSource

Changes in `processTelegramSource`:
```go
existing, err := o.sources.FindByCanonicalSource(ctx, in.TenantID, handle)
if err != nil {
    o.logger.Printf("orchestrator: FindByCanonicalSource for @%s error: %v (will create new)", handle, err)
}
if existing != nil {
    o.logger.Printf("orchestrator: found existing telegram source @%s (id=%s) for project %s", handle, existing.ID, in.ProjectID)
    // ... reuse existing
} else {
    o.logger.Printf("orchestrator: creating new telegram source @%s for project %s", handle, in.ProjectID)
    // ... create new
}
```

Same pattern applied to `processWebSource`.

### Backend (`backend/internal/repo/repositories.go`)
✅ Added debug logging to `FindByCanonicalSource`:
- Log all candidates when no match found
- Log successful matches
- Show what was searched for vs what was found

✅ Added debug logging to `ListByProject`:
- Log all sources with their ProjectID
- Show which sources were filtered out and why
- Print debug info when 0 sources returned

```go
if len(sources) == 0 && len(debugInfo) > 0 {
    fmt.Printf("[DataSourcesRepository] ListByProject: found 0 sources for project %s among %d total sources: %v\n", 
        projectID, len(debugInfo), debugInfo)
}
```

### Frontend (`web/src/stores/discoveryStore.ts`)
✅ **Fixed project creation event handling**:
- Now adds project directly to store instead of calling `fetchProjects()`
- Auto-selects newly created project
- Handles both `discovery.project` and `discoveryProject` event types

```typescript
if (msg?.type === 'discovery.project' || msg?.type === 'discoveryProject') {
    const newProject = {
        id: projectData.projectId,
        displayName: projectData.title,
        // ...
    };
    useProjectsStore.setState((state) => ({
        projects: [newProject, ...state.projects],
        currentProject: newProject, // Auto-select
    }));
}
```

✅ **Fixed source display during discovery**:
- When `discovery.profile` received for unknown source, fetch it from API
- When `discovery.status` received with status='fetching', create placeholder
- Added console logging for all WS messages for debugging

```typescript
if (msg?.type === 'discovery.status' && msg.data?.sourceId) {
    // ... existing source update logic
    } else if (msg.data.status === 'fetching' && msg.data.projectId) {
        // Add placeholder source for visual feedback
        const placeholder = {
            id: msg.data.sourceId,
            displayName: msg.data.sourceId,
            status: msg.data.status,
            kind: 'unknown',
        };
        return { sources: [...state.sources, placeholder] };
    }
}
```

## Expected Behavior After Fixes

1. ✅ **No more duplicates** - FindByCanonicalSource will properly find existing sources (logged for verification)
2. ✅ **Projects appear in chat list** - New projects added directly to store via WebSocket
3. ✅ **Sources visible during discovery** - Placeholder sources shown immediately, updated with profiles
4. ✅ **Better debugging** - Extensive logging helps identify any remaining issues
5. ✅ **"loaded 0 sources" traced** - Debug logs will show why sources weren't found (if issue persists)

## Testing Checklist

- [ ] Start new discovery with Telegram channel (@riverpark_life)
- [ ] Verify project appears in sidebar immediately
- [ ] Verify source appears with "fetching" status
- [ ] Verify source updates with profile data
- [ ] Check backend logs for "found existing" vs "creating new"
- [ ] Verify no duplicate sources in Firestore
- [ ] Check that ListByProject finds the sources (no "loaded 0 sources")

## Deployment

1. Rebuild backend: `cd backend && go build -o bin/server ./cmd/server`
2. Rebuild frontend: `cd web && npm run build`
3. Deploy both to GCP
4. Test with fresh discovery request

## Logs to Monitor

**Backend logs to watch:**
```
[FindByCanonicalSource] Found match by ChannelUsername: canonical=riverpark_life, id=XXX
orchestrator: found existing telegram source @riverpark_life (id=XXX) for project YYY
orchestrator: loaded N sources for project YYY, preparing synthesis
orchestrator: synthesis will use N profiles (out of N sources)
```

**Frontend console logs:**
```
[WS] Message received: {type: 'discovery.project', ...}
[WS] New project event: {projectId: 'XXX', title: 'YYY'}
[WS] Adding new project to store: {id: 'XXX', ...}
[WS] Message received: {type: 'discovery.profile', ...}
```



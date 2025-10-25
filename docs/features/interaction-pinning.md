# Interaction Pinning Feature

## Overview

Users can now "pin" assistant/agent interactions for quick reference and easy return. This feature allows marking important responses and keeping them easily accessible.

## Implementation Details

### Backend

#### Data Model (`backend/internal/repo/interactions.go`)

Added `InteractionMetadata` structure with `Pinned` field:

```go
// InteractionMetadata contains extensible metadata flags and settings
type InteractionMetadata struct {
    Pinned *bool `json:"pinned,omitempty" firestore:"pinned,omitempty"` // User-pinned for quick access
}

type Interaction struct {
    // ... other fields ...
    Metadata *InteractionMetadata `json:"metadata,omitempty" firestore:"metadata,omitempty"`
    // ... other fields ...
}
```

**Key Points:**
- `Pinned` is a pointer to `bool` for optional field handling
- Default value is `nil` (not set), treated as `false`
- Part of `Metadata` structure for future extensibility

#### API Endpoint

**Update Interaction** - `PATCH /api/v1/projects/{projectId}/interactions/{interactionId}`

Existing endpoint now supports `metadata` field updates:

```json
{
  "metadata": {
    "pinned": true
  }
}
```

**Whitelisted Fields** (`backend/internal/http/discovery.go:970`):
- `metadata` field is now allowed for updates

### Frontend

#### Type Definitions (`web/src/types/interaction.ts`)

```typescript
export interface InteractionMetadata {
  pinned?: boolean;
}

export interface Interaction {
  // ... other fields ...
  metadata?: InteractionMetadata;
  // ... other fields ...
}
```

#### API Client (`web/src/lib/api.ts`)

- `getProjectInteractions()` - Returns interactions with `metadata` field
- `updateProjectInteraction()` - Accepts `metadata` in update payload

#### Helper Functions (`web/src/lib/interactionHelpers.ts`)

```typescript
// Toggle pin status
toggleInteractionPin(projectId, interactionId, currentlyPinned)

// Set pin status explicitly
setInteractionPin(projectId, interactionId, true/false)

// Check if interaction is pinned
isInteractionPinned(interaction)
```

## Usage Examples

### Backend (Go)

**Create interaction with pinned flag:**
```go
metadata := &repo.InteractionMetadata{
    Pinned: &[]bool{true}[0],
}

interaction := &repo.Interaction{
    // ... other fields ...
    Metadata: metadata,
}
```

**Update existing interaction:**
```go
updates := map[string]interface{}{
    "metadata": map[string]interface{}{
        "pinned": true,
    },
}
interactionsRepo.Update(ctx, interactionID, updates)
```

### Frontend (TypeScript)

**Toggle pin on button click:**
```typescript
import { toggleInteractionPin, isInteractionPinned } from '@/lib/interactionHelpers';

const handlePinClick = async (interaction: Interaction) => {
  const currentlyPinned = isInteractionPinned(interaction);
  await toggleInteractionPin(
    projectId,
    interaction.id,
    currentlyPinned
  );
  // Refresh interactions list
  await refetchInteractions();
};
```

**Display pin status:**
```typescript
const isPinned = isInteractionPinned(interaction);

return (
  <div>
    {isPinned && <PinIcon />}
    {interaction.content}
  </div>
);
```

**Set pin explicitly:**
```typescript
import { setInteractionPin } from '@/lib/interactionHelpers';

// Pin interaction
await setInteractionPin(projectId, interactionId, true);

// Unpin interaction
await setInteractionPin(projectId, interactionId, false);
```

## UI Implementation ✅ Implemented

### Current Implementation (Hover Action)

Pin button appears on hover for assistant/agent messages only:

```
[Assistant Response]
Content goes here...
           [📌] [🗑️]  ← Pin and Delete buttons (top right)
```

**Features:**
- Pin icon button appears on hover (left of delete button)
- Only visible for assistant/agent messages (not user messages)
- Shows filled bookmark icon when pinned, outline when unpinned
- Clicking toggles pin state and updates server
- Smooth opacity transition on hover
- Consistent styling with delete button

**Location:** `web/src/pages/DiscoveryPage.tsx:3938-3959`

**Interaction Flow:**
1. User hovers over assistant message → Pin and Delete buttons appear
2. User clicks Pin button → Toggle pin state
3. Server updates `metadata.pinned`
4. UI refreshes to show new state (filled/outline icon)

### Future Enhancements

Potential additions:
- Pinned section at top of interactions list
- Filter toggle to show only pinned messages
- Keyboard shortcut for pinning (e.g., Ctrl+P)
- Visual indicator on message card when pinned (not just on hover)
- Bulk pin/unpin operations

## Benefits

1. **Easy Reference**: Quickly return to important responses
2. **Extensible**: `Metadata` structure allows future additions (favorites, tags, etc.)
3. **Backward Compatible**: Default `nil` value doesn't affect existing interactions
4. **Server-Persisted**: Pinned state syncs across devices

## Future Enhancements

The `Metadata` structure can be extended with:
- `favorite`: Boolean for favoriting
- `tags`: Array of user-defined tags
- `color`: Color coding for categorization
- `notes`: User notes about the interaction
- `lastViewed`: Timestamp for recently viewed tracking

Example:
```typescript
interface InteractionMetadata {
  pinned?: boolean;
  favorite?: boolean;
  tags?: string[];
  color?: string;
  notes?: string;
  lastViewed?: string;
}
```

## Testing

### Backend Tests
```go
// Test pinning interaction
func TestPinInteraction(t *testing.T) {
    // Create interaction
    it := &Interaction{Content: "test"}
    id, _ := repo.Create(ctx, it)
    
    // Pin it
    pinned := true
    updates := map[string]interface{}{
        "metadata": map[string]interface{}{
            "pinned": pinned,
        },
    }
    err := repo.Update(ctx, id, updates)
    assert.NoError(t, err)
    
    // Verify
    updated, _ := repo.Get(ctx, id)
    assert.NotNil(t, updated.Metadata)
    assert.True(t, *updated.Metadata.Pinned)
}
```

### Frontend Tests
```typescript
describe('Interaction Pinning', () => {
  it('should toggle pin status', async () => {
    const result = await toggleInteractionPin('proj1', 'int1', false);
    expect(result.success).toBe(true);
  });
  
  it('should check if interaction is pinned', () => {
    const pinned = { metadata: { pinned: true } };
    const notPinned = { metadata: { pinned: false } };
    const noPinned = {};
    
    expect(isInteractionPinned(pinned)).toBe(true);
    expect(isInteractionPinned(notPinned)).toBe(false);
    expect(isInteractionPinned(noPinned)).toBe(false);
  });
});
```

## Related Files

- `backend/internal/repo/interactions.go` - Data model
- `backend/internal/http/discovery.go` - API endpoint
- `web/src/types/interaction.ts` - TypeScript types
- `web/src/lib/api.ts` - API client
- `web/src/lib/interactionHelpers.ts` - Helper functions

## Migration

No migration required - backward compatible:
- Existing interactions without `metadata` field work as before
- `metadata` field is optional
- Default behavior (not pinned) when field is absent


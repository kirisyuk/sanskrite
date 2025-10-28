# Artifacts Data Model

## Overview

Artifacts are **dynamic documents** that are automatically generated and updated based on conversation context, pinned messages, and source documents. They extend the existing `CoreDoc` model rather than being a separate entity.

## Architecture Decision

**Artifact = Extended CoreDoc**

Artifacts are stored as `CoreDoc` objects with:
- `docType = "dynamic_artifact"`
- `artifactConfig` field containing generation configuration

This approach provides:
1. **Unified storage** - artifacts and static documents share the same infrastructure
2. **Reusable infrastructure** - upload/download, parsing, access control
3. **Natural evolution** - documents can become artifacts and vice versa
4. **Simpler API** - same endpoints for both document types

## Data Model

### CoreDoc Extensions

```go
type CoreDoc struct {
    // ... existing fields (ID, TenantID, ProjectID, Filename, etc.) ...
    
    // ExtractedText now stores the full assembled document (current version)
    ExtractedText *string
    
    // NEW: Artifact-specific fields
    DocType        *string         // "static" | "dynamic_artifact"
    ArtifactConfig *ArtifactConfig // nil for static docs
}
```

### ArtifactConfig

Contains all configuration for dynamic generation:

```go
type ArtifactConfig struct {
    // Document properties
    Format            string   // "text" | "table" | "report" | "presentation"
    Focus             string   // "brief" | "analysis" | "narrative" | "proposal" | "technical" | "executive"
    
    // AI models
    OrchestratorModel string   // Model for final review/assembly
    DefaultModel      string   // Default model for sections
    ReviewPrompt      string   // Prompt for orchestrator
    
    // Document structure
    Structure         []ArtifactSection       // Sections with prompts
    ContextSources    ArtifactContextSources  // What context to use
    
    // Auto-update
    AutoUpdateEnabled bool
    AutoUpdateConfig  *ArtifactAutoUpdate
    
    // Version management
    CurrentVersionID  string
    VersionsRetained  int                     // Default: 10
    Versions          []ArtifactVersion       // Sorted by timestamp desc
}
```

### ArtifactSection

Each section has its own generation config:

```go
type ArtifactSection struct {
    ID              string
    Title           string
    Order           int
    Prompt          string                  // Prompt for generating this section
    Model           *string                 // Override default model
    ContextOverride *ArtifactContextSources // Override context
    Content         *string                 // Current content
    GeneratedAt     *time.Time
    TokensUsed      *int
}
```

### ArtifactContextSources

Defines what context to use for generation:

```go
type ArtifactContextSources struct {
    ConversationRange *struct {
        From string // interaction ID
        To   string // interaction ID
    }
    Documents      []string // CoreDoc IDs
    SearchQuery    *string  // Search query for grounding
    PinnedMessages []string // Interaction IDs
}
```

### ArtifactVersion

Full snapshot of the artifact at a point in time:

```go
type ArtifactVersion struct {
    ID              string
    Timestamp       time.Time
    Trigger         string // "manual" | "auto" | "scheduled"
    
    // Full document
    FullDocument    string
    
    // Section snapshots (for cherry-picking)
    Sections        map[string]ArtifactSectionSnapshot
    
    // Context at generation time
    ContextSnapshot ArtifactContextSnapshot
    
    // Generation metadata
    GenerationMeta  ArtifactGenerationMeta
}
```

### ArtifactSectionSnapshot

Captures a section's state in a version:

```go
type ArtifactSectionSnapshot struct {
    Content     string
    Prompt      string     // Prompt used
    Model       string     // Model used
    TokensUsed  int
    GeneratedAt time.Time
    Quality     *float64   // 0-1 score
    QualityMeta *ArtifactQualityMeta
}
```

### ArtifactQualityMeta

Detailed quality metrics:

```go
type ArtifactQualityMeta struct {
    Coherence    float64 // Text coherence (0-1)
    Completeness float64 // Completeness of response (0-1)
    Relevance    float64 // Relevance to context (0-1)
}
```

## Version Management

### Version Retention

- **Default**: Keep last 10 versions
- **Configurable**: `versionsRetained` field
- **Smart retention** (future):
  - Always keep first version
  - Always keep last 5 versions
  - Compress older versions (one per day/week)

### Version Storage

**Current approach**: Full document in Firestore

**Future optimizations**:
1. Store only diff from previous version for old versions
2. Move old versions to GCS (cheaper storage)

### Cherry-Picking Sections

Users can:
1. View any previous version
2. Compare versions (diff)
3. Replace current section with section from previous version
4. Create hybrid version (mix sections from different versions)

## Auto-Update

### Triggers

```go
type ArtifactAutoUpdate struct {
    Trigger    string // "on_new_message" | "daily" | "weekly" | "on_new_document"
    Conditions *ArtifactUpdateConditions
    SmartUpdate *ArtifactSmartUpdateConfig
}
```

### Conditions

```go
type ArtifactUpdateConditions struct {
    MinNewMessages      *int     // >= N new messages
    MinContextChange    *float64 // >= X% context change (0-1)
    Keywords            []string // Trigger if these appear
    SearchQueryChanged  bool     // Trigger if search results changed
}
```

### Smart Update

```go
type ArtifactSmartUpdateConfig struct {
    CheckQuality        bool    // Check quality before replacing
    MinQualityThreshold float64 // Don't replace if worse (0-1)
    NotifyOnUpdate      bool    // Notify user
}
```

**Workflow**:
1. Trigger fires (e.g., new messages)
2. Generate new version
3. Evaluate quality
4. If quality >= threshold → auto-apply
5. If quality < threshold → notify user for review

## Frontend Types

Auto-generated from backend via JSON Schema:

```typescript
import type { CoreDoc, ArtifactContextSources } from '@/types';

// Type guards
import { isArtifact, isStaticDoc } from '@/types';

if (isArtifact(doc)) {
  // doc.artifactConfig is guaranteed to exist
  console.log(doc.artifactConfig.format);
}
```

### Utility Functions

```typescript
import {
  getQualityColor,
  getQualityLabel,
  calculateSectionQuality,
  shouldTriggerAutoUpdate,
  ArtifactFormatLabels,
  ArtifactFocusLabels,
} from '@/types';

// Display quality
const color = getQualityColor(0.85); // "hsl(120, 60%, 50%)"
const label = getQualityLabel(0.85); // "Good"

// Format display names
const formatName = ArtifactFormatLabels['report']; // "Report"
const focusName = ArtifactFocusLabels['analysis']; // "In-depth Analysis"
```

## API Endpoints

Artifacts use the same endpoints as CoreDoc:

- `GET /api/v1/projects/:projectId/docs` - List all docs (filter by `docType`)
- `GET /api/v1/projects/:projectId/docs/:docId` - Get specific doc/artifact
- `POST /api/v1/projects/:projectId/docs` - Create new doc/artifact
- `PATCH /api/v1/projects/:projectId/docs/:docId` - Update doc/artifact
- `DELETE /api/v1/projects/:projectId/docs/:docId` - Delete doc/artifact

**New endpoints** (future):
- `POST /api/v1/projects/:projectId/docs/:docId/generate` - Generate new version
- `POST /api/v1/projects/:projectId/docs/:docId/versions/:versionId/restore` - Restore version
- `POST /api/v1/projects/:projectId/docs/:docId/sections/:sectionId/cherry-pick` - Replace section

## UI Components

### Workspace Artifacts Panel

Located on the right side (pastel green button), displays:
1. List of artifacts in project
2. "+ Create Artifact" button
3. Artifact preview cards with:
   - Format & Focus badges
   - Last updated timestamp
   - Version indicator
   - Quality score (if available)

### Artifact Editor (future)

- Section-based editing
- Context source selector
- Model configuration
- Version history viewer
- Quality metrics dashboard

### Version Comparison (future)

- Side-by-side diff view
- Section-level comparison
- Quality delta indicators
- Cherry-pick buttons

## Implementation Phases

### Phase 1: Data Model ✅
- [x] Backend types (Go)
- [x] Frontend types (TypeScript)
- [x] JSON Schema generation
- [x] Type utilities

### Phase 2: Basic CRUD (next)
- [ ] Create artifact endpoint
- [ ] List/filter artifacts
- [ ] Update artifact config
- [ ] Delete artifact

### Phase 3: Generation (future)
- [ ] Section generation
- [ ] Orchestrator assembly
- [ ] Version creation
- [ ] Quality evaluation

### Phase 4: Auto-Update (future)
- [ ] Trigger detection
- [ ] Condition evaluation
- [ ] Smart update logic
- [ ] User notifications

### Phase 5: Advanced Features (future)
- [ ] Version comparison
- [ ] Cherry-picking
- [ ] Hybrid versions
- [ ] Storage optimization

## Related Documents

- [Data Model](./data-model.md) - Overall data model
- [Backend Modules: Discovery](./backend/modules/discovery.md) - Context grounding
- [Frontend Architecture](./frontend-architecture-principles.md) - UI patterns


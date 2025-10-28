# Artifacts Quick Start

## What are Artifacts?

**Artifacts** are dynamic documents that automatically generate and update based on your conversation context. Think of them as "living documents" that evolve with your project.

## Key Concepts

### 1. Artifact = Extended CoreDoc

Artifacts are stored as regular documents with special configuration:
- `docType: "dynamic_artifact"`
- `artifactConfig: { ... }` - generation settings

### 2. Structure-Based Generation

Each artifact has **sections** with individual prompts:
```
Artifact: "Project Summary"
├─ Executive Summary (prompt: "Summarize key decisions...")
├─ Key Findings (prompt: "Extract main insights...")
└─ Recommendations (prompt: "Suggest next steps...")
```

### 3. Context Sources

Artifacts pull context from:
- **Conversation range** - specific messages
- **Documents** - attached files
- **Search query** - grounded search results
- **Pinned messages** - user-marked important messages

### 4. Version History

Every generation creates a new version:
- Full document snapshot
- Section-level snapshots
- Quality scores
- Context snapshot

You can:
- View any previous version
- Compare versions
- Cherry-pick better sections from old versions
- Restore entire old versions

### 5. Auto-Update

Artifacts can automatically regenerate when:
- New messages arrive
- New documents added
- Search results change
- Specific keywords appear

**Smart update** checks quality before replacing - won't auto-update if new version is worse!

## Usage Examples

### Example 1: Meeting Summary

```typescript
const artifact: CoreDoc = {
  docType: "dynamic_artifact",
  filename: "Weekly Standup Summary",
  artifactConfig: {
    format: "report",
    focus: "brief",
    orchestratorModel: "claude-3-5-sonnet-20241022",
    defaultModel: "claude-3-5-sonnet-20241022",
    reviewPrompt: "Review and ensure consistency",
    structure: [
      {
        id: "decisions",
        title: "Key Decisions",
        order: 1,
        prompt: "Extract all decisions made in the conversation"
      },
      {
        id: "action-items",
        title: "Action Items",
        order: 2,
        prompt: "List all action items with owners"
      },
      {
        id: "blockers",
        title: "Blockers",
        order: 3,
        prompt: "Identify any blockers or risks mentioned"
      }
    ],
    contextSources: {
      conversationRange: {
        from: "msg_001",
        to: "msg_050"
      },
      pinnedMessages: ["msg_012", "msg_034"]
    },
    autoUpdateEnabled: true,
    autoUpdateConfig: {
      trigger: "on_new_message",
      conditions: {
        minNewMessages: 5
      }
    },
    versionsRetained: 10,
    currentVersionID: "v1",
    versions: []
  }
}
```

### Example 2: Research Analysis

```typescript
const artifact: CoreDoc = {
  docType: "dynamic_artifact",
  filename: "Competitor Analysis",
  artifactConfig: {
    format: "report",
    focus: "analysis",
    orchestratorModel: "claude-3-5-sonnet-20241022",
    defaultModel: "claude-3-5-sonnet-20241022",
    reviewPrompt: "Ensure analysis is balanced and data-driven",
    structure: [
      {
        id: "overview",
        title: "Market Overview",
        order: 1,
        prompt: "Summarize the competitive landscape"
      },
      {
        id: "strengths",
        title: "Competitive Strengths",
        order: 2,
        prompt: "Analyze competitors' key strengths"
      },
      {
        id: "opportunities",
        title: "Opportunities",
        order: 3,
        prompt: "Identify market opportunities"
      }
    ],
    contextSources: {
      documents: ["doc_001", "doc_002", "doc_003"],
      searchQuery: "competitor analysis 2024"
    },
    autoUpdateEnabled: true,
    autoUpdateConfig: {
      trigger: "on_new_document",
      smartUpdate: {
        checkQuality: true,
        minQualityThreshold: 0.75,
        notifyOnUpdate: true
      }
    },
    versionsRetained: 20,
    currentVersionID: "v1",
    versions: []
  }
}
```

### Example 3: Technical Proposal

```typescript
const artifact: CoreDoc = {
  docType: "dynamic_artifact",
  filename: "Architecture Proposal",
  artifactConfig: {
    format: "report",
    focus: "proposal",
    orchestratorModel: "claude-3-5-sonnet-20241022",
    defaultModel: "claude-3-5-sonnet-20241022",
    reviewPrompt: "Ensure technical accuracy and clarity",
    structure: [
      {
        id: "problem",
        title: "Problem Statement",
        order: 1,
        prompt: "Clearly define the problem we're solving"
      },
      {
        id: "solution",
        title: "Proposed Solution",
        order: 2,
        prompt: "Describe the technical solution in detail"
      },
      {
        id: "tradeoffs",
        title: "Trade-offs",
        order: 3,
        prompt: "Analyze pros, cons, and alternatives"
      },
      {
        id: "implementation",
        title: "Implementation Plan",
        order: 4,
        prompt: "Outline implementation steps and timeline"
      }
    ],
    contextSources: {
      conversationRange: {
        from: "msg_100",
        to: "msg_250"
      },
      documents: ["tech_spec_001"],
      pinnedMessages: ["msg_120", "msg_180", "msg_230"]
    },
    autoUpdateEnabled: false, // Manual updates only
    versionsRetained: 15,
    currentVersionID: "v3",
    versions: [
      {
        id: "v3",
        timestamp: "2024-10-25T10:00:00Z",
        trigger: "manual",
        fullDocument: "...",
        sections: { /* ... */ },
        contextSnapshot: { /* ... */ },
        generationMeta: {
          totalTokensUsed: 15000,
          durationMs: 45000,
          sectionsGenerated: 4,
          errors: []
        }
      },
      // ... older versions
    ]
  }
}
```

## Frontend Usage

### Type Guards

```typescript
import { isArtifact, isStaticDoc } from '@/types';

// Check if document is an artifact
if (isArtifact(doc)) {
  console.log('Artifact format:', doc.artifactConfig.format);
  console.log('Sections:', doc.artifactConfig.structure.length);
}

// Check if document is static
if (isStaticDoc(doc)) {
  console.log('Static document:', doc.filename);
}
```

### Display Helpers

```typescript
import {
  ArtifactFormatLabels,
  ArtifactFocusLabels,
  getQualityColor,
  getQualityLabel,
} from '@/types';

// Display format
const formatName = ArtifactFormatLabels[artifact.artifactConfig.format];
// "Report"

// Display focus
const focusName = ArtifactFocusLabels[artifact.artifactConfig.focus];
// "In-depth Analysis"

// Display quality
const quality = version.sections['overview'].quality || 0;
const color = getQualityColor(quality); // "hsl(120, 60%, 50%)"
const label = getQualityLabel(quality); // "Good"
```

### Quality Calculation

```typescript
import { calculateSectionQuality } from '@/types';

const section = version.sections['overview'];
if (section.qualityMeta) {
  const overallQuality = calculateSectionQuality(
    section.qualityMeta.coherence,
    section.qualityMeta.completeness,
    section.qualityMeta.relevance
  );
  console.log('Overall quality:', overallQuality); // 0.85
}
```

### Auto-Update Check

```typescript
import { shouldTriggerAutoUpdate } from '@/types';

const metrics = {
  newMessagesCount: 10,
  newDocumentsCount: 2,
  contextChangePercent: 0.35,
  keywordsMatched: ['decision', 'architecture'],
  searchResultsChanged: true
};

if (shouldTriggerAutoUpdate(metrics, artifact.artifactConfig.autoUpdateConfig)) {
  console.log('Should trigger auto-update!');
}
```

## Backend Implementation (Future)

### Generation Service

```go
type ArtifactService struct {
    aiExecutor *ai.TaskExecutor
    repo       *repo.CoreDocsRepository
}

func (s *ArtifactService) GenerateVersion(
    ctx context.Context,
    artifact *models.CoreDoc,
) (*models.ArtifactVersion, error) {
    // 1. Gather context
    context := s.gatherContext(artifact.ArtifactConfig.ContextSources)
    
    // 2. Generate each section
    sections := make(map[string]models.ArtifactSectionSnapshot)
    for _, section := range artifact.ArtifactConfig.Structure {
        content, err := s.generateSection(ctx, section, context)
        if err != nil {
            return nil, err
        }
        sections[section.ID] = content
    }
    
    // 3. Orchestrator assembly
    fullDoc, err := s.assembleDocument(ctx, sections, artifact.ArtifactConfig)
    if err != nil {
        return nil, err
    }
    
    // 4. Create version
    version := &models.ArtifactVersion{
        ID:           generateID(),
        Timestamp:    time.Now(),
        Trigger:      "manual",
        FullDocument: fullDoc,
        Sections:     sections,
        // ...
    }
    
    return version, nil
}
```

## Next Steps

1. **Phase 2**: Implement basic CRUD endpoints
2. **Phase 3**: Build generation service
3. **Phase 4**: Add auto-update logic
4. **Phase 5**: Build UI components

See [Artifacts Data Model](./artifacts-data-model.md) for full details.


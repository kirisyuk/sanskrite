# Frontend Type Organization

## Overview

Frontend TypeScript types are organized into two categories:
1. **Auto-generated types** from `backend/internal/models/`
2. **Manually maintained types** from `backend/internal/repo/`

## Auto-Generated Types

### Source
- Go package: `backend/internal/models/`
- JSON Schema files: `web/src/types/schemas/*.schema.json`
- Generated types: `web/src/types/generated.ts`

### How It Works

1. **Backend**: Go structs in `models` package are annotated with JSON tags
2. **Schema Generation**: 
   ```bash
   cd backend/internal/tools/schemagen
   go run main.go
   ```
   This generates JSON Schema files in `web/src/types/schemas/`

3. **Type Generation**:
   ```bash
   cd web
   npm run gen:types
   ```
   This converts JSON Schemas to TypeScript interfaces in `generated.ts`

### Current Auto-Generated Types
- `User` - User account model
- `Project` - Project model
- `DataSource` - Data source model
- `Document` - Document model  
- `SourceProfile` - Source profile model

### Adding New Auto-Generated Types

1. Add struct to `backend/internal/models/models.go`
2. Add to targets in `backend/internal/tools/schemagen/main.go`:
   ```go
   {File: "NewModel.schema.json", Val: new(models.NewModel)}
   ```
3. Run schema generation: `cd backend/internal/tools/schemagen && go run main.go`
4. Run type generation: `cd web && npm run gen:types`
5. Export in `web/src/types/index.ts`

## Manually Maintained Types

### Source
- Go package: `backend/internal/repo/`
- TypeScript files: `web/src/types/*.ts` (except `generated.ts`)

### Why Manual?

The `repo` package contains repository-level models that:
- Are not part of the core domain models
- Include Firestore-specific structures
- Mix domain logic with persistence concerns
- Would require complex JSON Schema handling

### Current Manually Maintained Types

#### Interaction Types (`web/src/types/interaction.ts`)
Source: `backend/internal/repo/interactions.go`

```typescript
export interface Interaction {
  id: string;
  tenantId: string;
  projectId: string;
  kind: string;
  content: string;
  metadata?: InteractionMetadata;
  // ... other fields
}

export interface InteractionMetadata {
  pinned?: boolean;
}
```

**Synchronization Process:**
1. Update `backend/internal/repo/interactions.go`
2. Manually update corresponding TypeScript types in `web/src/types/interaction.ts`
3. Update API client types in `web/src/lib/api.ts` if needed
4. Document changes in this file

### Adding New Manually Maintained Types

1. Create new file in `web/src/types/[name].ts`
2. Add JSDoc comment explaining why it's manual (see `interaction.ts` example)
3. Export types in `web/src/types/index.ts` under "Manually maintained types" section
4. Document in this file

## Type Export Structure

`web/src/types/index.ts` is organized into sections:

```typescript
// === Auto-generated types ===
// Generated via: npm run gen:types
export type { User, Project, DataSource } from './generated';

// === Manually maintained types ===
// Manually synced with backend/internal/repo
export type { Interaction, InteractionMetadata } from './interaction';
```

## Best Practices

### For Auto-Generated Types

✅ **DO:**
- Keep models in `backend/internal/models/`
- Use JSON tags for all fields
- Run both schema and type generation after changes
- Commit generated files to version control

❌ **DON'T:**
- Manually edit `generated.ts`
- Manually edit `.schema.json` files
- Forget to regenerate after backend changes

### For Manually Maintained Types

✅ **DO:**
- Add clear JSDoc comments explaining why manual
- Keep types in sync with backend structs
- Use the same field names and types as backend
- Add changes to PR description

❌ **DON'T:**
- Add types that could be auto-generated
- Diverge from backend field names/types
- Skip documentation

## Type Synchronization Checklist

### After Backend Model Changes

**Auto-Generated Types:**
- [ ] Update Go struct in `backend/internal/models/`
- [ ] Run `cd backend/internal/tools/schemagen && go run main.go`
- [ ] Run `cd web && npm run gen:types`
- [ ] Verify no TypeScript errors
- [ ] Test affected components

**Manual Types:**
- [ ] Update Go struct in `backend/internal/repo/`
- [ ] Update corresponding TypeScript file in `web/src/types/`
- [ ] Update API client types if needed
- [ ] Verify no TypeScript errors
- [ ] Test affected components
- [ ] Document changes

## Common Issues

### Issue: Types Out of Sync
**Symptom:** TypeScript errors about missing/wrong fields  
**Solution:** Re-run type generation or manually sync

### Issue: Generated types have wrong names
**Symptom:** Long auto-generated interface names  
**Solution:** Check JSON Schema `title` field or use type aliases

### Issue: Missing fields in manual types
**Symptom:** Runtime errors or undefined values  
**Solution:** Compare with backend Go struct, add missing fields

## Future Improvements

### Potential Enhancements

1. **Automated Sync Detection**
   - CI check comparing Go structs with TypeScript types
   - Warning when manual types drift from backend

2. **Unified Generation**
   - Move repo models to models package where appropriate
   - Generate all types automatically

3. **Type Validation**
   - Runtime type checking with Zod or similar
   - Automatic validation schema generation

4. **Documentation Generation**
   - Auto-generate type documentation from Go comments
   - Keep TypeScript JSDoc in sync

## Related Files

- `backend/internal/models/models.go` - Core domain models
- `backend/internal/repo/interactions.go` - Repository models
- `backend/internal/tools/schemagen/main.go` - Schema generator
- `web/scripts/generate-types.mjs` - Type generator
- `web/src/types/index.ts` - Type exports
- `web/src/types/generated.ts` - Auto-generated types
- `web/src/types/interaction.ts` - Manual Interaction types

## Questions?

For questions about:
- **Type generation process** - See `web/scripts/README.md`
- **JSON Schema details** - See `jsonschema` package docs
- **Why a type is manual** - Check JSDoc comment in type file


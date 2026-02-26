# Vertex AI Search Integration

## Quick Start

Пу### 1. Configure Environment Variables
Set these in your deployment:
- `VERTEX_SEARCH_DATA_STORE_ID=rag-documents-store`
- `VERTEX_SEARCH_LOCATION=global`
- `GEMINI_API_KEY=<your-key>`
- `RAG_DOCUMENTS_BUCKET=h1horizon-488615-rag-documents`

Backend automatically sets up sync - no manual setup needed!

### 2. Test Search
```bash
./scripts/test-vertex-search.sh -p sanskrite
```

### 3. Use in Application

**Backend** (automatic tenant filtering):
```go
// Already implemented in backend/internal/http/discovery.go
// POST /api/v1/projects/{projectId}/vertex-search
searchResp, err := h.vertexSearchClient.Search(ctx, searchReq)
```

**Frontend**:
```typescript
const results = await fetch('/api/v1/projects/{projectId}/vertex-search', {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${token}`,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    query: 'search query',
    pageSize: 10
  })
});
```

## Key Features

- ✅ **Automatic Sync**: Documents sync from GCS to Vertex AI Search automatically
- ✅ **Multi-Tenancy**: Strong tenant isolation with automatic filtering
- ✅ **Global Documents**: Shared system documents accessible to all tenants
- ✅ **Embeddings**: Automatic embedding creation by Vertex AI
- ✅ **Real-time**: Documents searchable within 1-2 minutes of upload

## Architecture

See [vertex-search-multi-tenancy.md](./vertex-search-multi-tenancy.md) for detailed architecture.

## Monitoring

```bash
# View backend logs (sync happens in backend)
# Look for "[VertexAISearch] Sync triggered" messages

# Check indexed documents
./scripts/test-vertex-search.sh
```

## Troubleshooting

**Documents not appearing?**
1. Check Cloud Function logs
2. Verify GCS metadata contains tenantId
3. Run manual sync:
   ```bash
   ACCESS_TOKEN=$(gcloud auth print-access-token)
   curl -X POST \
     -H "Authorization: Bearer $ACCESS_TOKEN" \
     -H "Content-Type: application/json" \
     "https://discoveryengine.googleapis.com/v1beta/projects/h1horizon-488615/locations/global/collections/default_collection/dataStores/rag-documents-store/branches/default_branch/documents:import" \
     -d '{"gcsSource": {"inputUris": ["gs://h1horizon-488615-rag-documents/**"], "dataSchema": "content"}, "reconciliationMode": "INCREMENTAL"}'
   ```

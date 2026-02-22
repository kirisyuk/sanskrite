package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// CombineFragmentsInLens merges a "duplicate" fragment into a "keep" fragment:
// - merges fragment data (Labels, Identifiers, State, merged_paths) from remove -> keep
// - rewires global DependencyEdges (tenantId="global") from remove -> keep
// - updates lens scope and lens items
// - deletes the remove fragment
// - records merge audit for traceability
//
// POST /api/horizon/lenses/{lensId}/fragments:combine
func (h *HorizonHandlers) CombineFragmentsInLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		httputil.WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract lensID from path: /api/horizon/lenses/{lensId}/fragments:combine
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	pathParts := strings.Split(path, "/")
	if len(pathParts) < 2 || strings.TrimSpace(pathParts[0]) == "" {
		httputil.WriteBadRequest(w, "INVALID_PATH", "lensId required in path")
		return
	}
	lensID := strings.TrimSpace(pathParts[0])

	var body struct {
		KeepFragmentID   string `json:"keepFragmentId"`
		RemoveFragmentID string `json:"removeFragmentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}
	keepID := strings.TrimSpace(body.KeepFragmentID)
	removeID := strings.TrimSpace(body.RemoveFragmentID)
	if keepID == "" || removeID == "" {
		httputil.WriteBadRequest(w, "VALIDATION_ERROR", "keepFragmentId and removeFragmentId are required")
		return
	}
	if keepID == removeID {
		httputil.WriteBadRequest(w, "VALIDATION_ERROR", "keepFragmentId and removeFragmentId must be different")
		return
	}

	// Load lens and verify access
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil || lens == nil {
		httputil.WriteNotFound(w, "Lens not found")
		return
	}
	if lens.TenantID != authInfo.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	// Load both fragments for merge and paths
	fragmentsRepo := h.service.Fragments()
	keepFrag, err := fragmentsRepo.Get(r.Context(), keepID)
	if err != nil || keepFrag == nil {
		httputil.WriteNotFound(w, "Keep fragment not found")
		return
	}
	removeFrag, err := fragmentsRepo.Get(r.Context(), removeID)
	if err != nil || removeFrag == nil {
		httputil.WriteNotFound(w, "Remove fragment not found")
		return
	}
	keepPath := strings.TrimSpace(keepFrag.CanonicalPath)
	removePath := strings.TrimSpace(removeFrag.CanonicalPath)
	tenantID := strings.TrimSpace(keepFrag.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	// Merge via single entry point (data, edges, delete, audit)
	var mergeRes struct {
		DataMerged      bool
		EdgesReassigned int
		EdgesDeleted    int
	}
	if h.mergeFragmentsRepo != nil {
		res, err := h.mergeFragmentsRepo.MergeFragment(r.Context(), keepID, removeID, tenantID, "lens_combine", "", "", lensID)
		if err != nil {
			httputil.WriteInternalError(w, "Merge failed: "+err.Error())
			return
		}
		mergeRes.DataMerged = res.DataMerged
		mergeRes.EdgesReassigned = res.EdgesReassigned
		mergeRes.EdgesDeleted = res.EdgesDeleted
	} else {
		httputil.WriteInternalError(w, "Merge repository not configured")
		return
	}

	// Update lens scope: if removePath present -> remove; ensure keepPath exists (insert in place if missing).
	scopeChanged := false
	if removePath != "" && keepPath != "" {
		idx := -1
		for i, p := range lens.Scope {
			if strings.TrimSpace(p) == removePath {
				idx = i
				break
			}
		}
		if idx >= 0 {
			// Remove the duplicate path
			next := make([]string, 0, len(lens.Scope))
			for i, p := range lens.Scope {
				if i == idx {
					continue
				}
				next = append(next, p)
			}
			// Ensure keepPath exists
			hasKeep := false
			for _, p := range next {
				if strings.TrimSpace(p) == keepPath {
					hasKeep = true
					break
				}
			}
			if !hasKeep {
				// Insert keepPath where removePath used to be (best UX: preserves user ordering).
				if idx < 0 {
					next = append(next, keepPath)
				} else if idx >= len(next) {
					next = append(next, keepPath)
				} else {
					out := make([]string, 0, len(next)+1)
					out = append(out, next[:idx]...)
					out = append(out, keepPath)
					out = append(out, next[idx:]...)
					next = out
				}
			}
			lens.Scope = next
			scopeChanged = true
		}
	}
	if scopeChanged {
		if err := h.service.Lens().UpdateLens(r.Context(), lens); err != nil {
			httputil.WriteInternalError(w, "Failed to update lens scope")
			return
		}
	}

	// 3) Update lens items (best-effort, but should succeed).
	updatedItems := 0
	deletedItems := 0
	if h.lensItemsHandler != nil && h.lensItemsHandler.lensItemsRepo != nil {
		items, err := h.lensItemsHandler.lensItemsRepo.ListByLens(r.Context(), lensID)
		if err == nil {
			// For deduping edge items after rewrite.
			seenEdgeKey := map[string]string{} // key -> itemID (kept)

			for _, it := range items {
				if it == nil || strings.TrimSpace(it.ID) == "" {
					continue
				}
				switch strings.TrimSpace(it.Kind) {
				case "node":
					fragMatch := it.FragmentID != nil && strings.TrimSpace(*it.FragmentID) == removeID
					pathMatch := it.CanonicalPath != nil && removePath != "" && strings.TrimSpace(*it.CanonicalPath) == removePath
					if fragMatch || pathMatch {
						_ = h.lensItemsHandler.lensItemsRepo.DeleteByID(r.Context(), it.ID)
						deletedItems++
					}
				case "edge":
					if it.SourceFragmentID == nil || it.TargetFragmentID == nil {
						continue
					}
					src := strings.TrimSpace(*it.SourceFragmentID)
					tgt := strings.TrimSpace(*it.TargetFragmentID)
					changed := false
					if src == removeID {
						src = keepID
						changed = true
					}
					if tgt == removeID {
						tgt = keepID
						changed = true
					}

					if changed && src != "" && src == tgt {
						_ = h.lensItemsHandler.lensItemsRepo.DeleteByID(r.Context(), it.ID)
						deletedItems++
						continue
					}

					etype := ""
					if it.Subkind != nil {
						etype = strings.TrimSpace(*it.Subkind)
					}
					if etype == "" && it.EdgeType != nil {
						etype = strings.TrimSpace(*it.EdgeType)
					}
					if etype == "" {
						etype = "EDGE"
					}

					// Dedup items: keep first, delete later duplicates.
					key := src + "|" + tgt + "|" + etype
					if kept, ok := seenEdgeKey[key]; ok && kept != "" && kept != it.ID {
						_ = h.lensItemsHandler.lensItemsRepo.DeleteByID(r.Context(), it.ID)
						deletedItems++
						continue
					}
					seenEdgeKey[key] = it.ID

					if changed {
						updates := map[string]interface{}{
							"sourceFragmentId": src,
							"targetFragmentId": tgt,
						}
						if err := h.lensItemsHandler.lensItemsRepo.Update(r.Context(), it.ID, updates); err == nil {
							updatedItems++
						}
					}
				}
			}
		}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"lensId":              lensID,
		"keepFragmentId":      keepID,
		"removeFragmentId":    removeID,
		"scopeChanged":        scopeChanged,
		"keepCanonicalPath":   keepPath,
		"removeCanonicalPath": removePath,
		"fragmentMerged":      mergeRes.DataMerged,
		"edges": map[string]any{
			"updated": mergeRes.EdgesReassigned,
			"deleted": mergeRes.EdgesDeleted,
		},
		"lensItems": map[string]any{
			"updated": updatedItems,
			"deleted": deletedItems,
		},
	})
}



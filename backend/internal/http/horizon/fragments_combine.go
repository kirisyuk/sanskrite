package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// CombineFragmentsInLens merges a "duplicate" fragment into a "keep" fragment:
// - rewires global DependencyEdges (tenantId="global") from duplicate -> keep
// - updates lens scope: if scope contains duplicate canonical path, remove it and ensure keep canonical path exists
// - updates lens items: delete duplicate node item and rewrite edge items from duplicate -> keep
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

	// Resolve canonical paths (best-effort) to update scope
	var keepPath, removePath string
	if frag, err := h.service.Fragments().Get(r.Context(), keepID); err == nil && frag != nil {
		keepPath = strings.TrimSpace(frag.CanonicalPath)
	}
	if frag, err := h.service.Fragments().Get(r.Context(), removeID); err == nil && frag != nil {
		removePath = strings.TrimSpace(frag.CanonicalPath)
	}

	// 1) Rewire global dependency edges (tenantId="global") from removeID -> keepID.
	edgesRepo := h.service.Edges()
	updatedEdges := 0
	deletedEdges := 0
	{
		byID := map[string]*horizonmodels.DependencyEdge{}
		outgoing, _ := edgesRepo.ListBySource(r.Context(), "global", removeID, nil, 5000)
		incoming, _ := edgesRepo.ListByTarget(r.Context(), "global", removeID, nil, 5000)
		for _, e := range outgoing {
			if e != nil && strings.TrimSpace(e.ID) != "" {
				byID[e.ID] = e
			}
		}
		for _, e := range incoming {
			if e != nil && strings.TrimSpace(e.ID) != "" {
				byID[e.ID] = e
			}
		}

		for _, e := range byID {
			if e == nil || strings.TrimSpace(e.ID) == "" {
				continue
			}
			ns := strings.TrimSpace(e.SourceFragmentID)
			nt := strings.TrimSpace(e.TargetFragmentID)
			changed := false
			if ns == removeID {
				ns = keepID
				changed = true
			}
			if nt == removeID {
				nt = keepID
				changed = true
			}
			if !changed {
				continue
			}

			// Avoid self-loops after rewrite.
			if ns != "" && ns == nt {
				_ = edgesRepo.Delete(r.Context(), e.ID)
				deletedEdges++
				continue
			}

			// Avoid duplicates (source,target,type) after rewrite.
			if existing, err := edgesRepo.GetBySourceTargetAndType(r.Context(), "global", ns, nt, e.Type); err == nil && existing != nil && existing.ID != "" && existing.ID != e.ID {
				_ = edgesRepo.Delete(r.Context(), e.ID)
				deletedEdges++
				continue
			}

			e.SourceFragmentID = ns
			e.TargetFragmentID = nt
			if err := edgesRepo.Update(r.Context(), e); err == nil {
				updatedEdges++
			}
		}
	}

	// 2) Update lens scope: if removePath present -> remove; ensure keepPath exists (insert in place if missing).
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
		"ok":              true,
		"lensId":          lensID,
		"keepFragmentId":  keepID,
		"removeFragmentId": removeID,
		"scopeChanged":    scopeChanged,
		"keepCanonicalPath": keepPath,
		"removeCanonicalPath": removePath,
		"edges": map[string]any{
			"updated": updatedEdges,
			"deleted": deletedEdges,
		},
		"lensItems": map[string]any{
			"updated": updatedItems,
			"deleted": deletedItems,
		},
	})
}



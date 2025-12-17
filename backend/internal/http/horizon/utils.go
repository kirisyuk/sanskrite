package horizon

import (
	"errors"
	"net/http"
	"strings"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// extractIDFromPath извлекает ID из URL пути
// Пример: /api/horizon/lenses/{id} -> {id}
func extractIDFromPath(r *http.Request, prefix string) string {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	pathParts := strings.Split(path, "/")
	if len(pathParts) > 0 && pathParts[0] != "" {
		return pathParts[0]
	}
	return ""
}

// requireAuth проверяет аутентификацию и возвращает AuthInfo
func requireAuth(r *http.Request) (*httputil.AuthInfo, error) {
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		return nil, errors.New("authentication required")
	}
	return authInfo, nil
}


package ai

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// TrackImportOperation отслеживает операцию импорта Vertex AI Search и обновляет статус документов после завершения
// operationName: формат projects/{project}/locations/{location}/collections/{collection}/dataStores/{dataStore}/branches/{branch}/operations/{operationId}
// tenantID: ID tenant для обновления документов
// docsRepo: репозиторий документов для обновления статуса
// maxWaitTime: максимальное время ожидания завершения операции (по умолчанию 5 минут)
func TrackImportOperation(
	ctx context.Context,
	vertexClient *VertexAISearchClient,
	docsRepo *repo.DocumentsRepository,
	operationName string,
	tenantID string,
	maxWaitTime time.Duration,
) error {
	if maxWaitTime == 0 {
		maxWaitTime = 5 * time.Minute
	}

	deadline := time.Now().Add(maxWaitTime)
	checkInterval := 10 * time.Second // Проверяем каждые 10 секунд

	log.Printf("[VertexAISearchTracker] Starting to track operation: %s", operationName)

	for time.Now().Before(deadline) {
		status, err := vertexClient.CheckOperationStatus(ctx, operationName)
		if err != nil {
			log.Printf("[VertexAISearchTracker] Failed to check operation status: %v", err)
			time.Sleep(checkInterval)
			continue
		}

		if status.Done {
			if status.Error != nil {
				return fmt.Errorf("import operation failed: code=%d, message=%s", status.Error.Code, status.Error.Message)
			}

			// Operation completed successfully - mark documents as indexed
			log.Printf("[VertexAISearchTracker] Operation completed successfully, marking documents as indexed for tenant %s", tenantID)
			if err := docsRepo.MarkTenantDocumentsAsIndexed(ctx, tenantID); err != nil {
				return fmt.Errorf("failed to mark documents as indexed: %w", err)
			}

			log.Printf("[VertexAISearchTracker] Successfully marked documents as indexed for tenant %s", tenantID)
			return nil
		}

		// Operation still in progress
		log.Printf("[VertexAISearchTracker] Operation still in progress, checking again in %v", checkInterval)
		time.Sleep(checkInterval)
	}

	return fmt.Errorf("operation tracking timeout after %v", maxWaitTime)
}

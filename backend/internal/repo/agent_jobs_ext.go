// ListPendingByAgent возвращает список ожидающих задач для конкретного агента по всем тенантам
// Использует Collection Group query для поиска по всем коллекциям agent_jobs
func (r *AgentJobsRepository) ListPendingByAgent(ctx context.Context, agentID string, limit int) ([]*models.AgentJob, error) {
	if agentID == "" {
		return nil, ErrInvalidAgentJob
	}
	
	// Используем Collection Group query
	// Требуется индекс для collection group "agent_jobs": status ASC, createdAt ASC (или просто по status/agentId)
	q := r.db.CollectionGroup("agent_jobs").
		Where("agentId", "==", agentID).
		Where("status", "==", models.AgentJobStatusPending).
		OrderBy("createdAt", firestore.Asc) // FIFO
		
	if limit > 0 {
		q = q.Limit(limit)
	}
	
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	
	jobs := make([]*models.AgentJob, 0, len(docs))
	for _, doc := range docs {
		var job models.AgentJob
		if err := doc.DataTo(&job); err != nil {
			continue
		}
		job.ID = doc.Ref.ID
		jobs = append(jobs, &job)
	}
	
	return jobs, nil
}


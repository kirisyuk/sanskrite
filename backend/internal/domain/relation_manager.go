package domain

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DomainRelationManager управляет связями объектов с доменами
type DomainRelationManager struct {
	relationsRepo *repo.RelationRepository
	logger        *log.Logger
}

// NewDomainRelationManager создает новый менеджер связей доменов
func NewDomainRelationManager(
	relationsRepo *repo.RelationRepository,
	logger *log.Logger,
) *DomainRelationManager {
	return &DomainRelationManager{
		relationsRepo: relationsRepo,
		logger:        logger,
	}
}

// LinkEntityToDomain создает Relation между Entity и Domain Concept
func (m *DomainRelationManager) LinkEntityToDomain(
	ctx context.Context,
	tenantID string,
	entityID string,
	domainConceptID string,
	confidence float64,
	relevance string,
) error {
	now := time.Now().UTC()

	relation := &models.Relation{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "system",
			UpdatedBy: "system",
			Labels:    map[string]string{"en": "belongsToDomain"},
			Status:    "active",
		},
		SubjectID:  entityID,
		Predicate:  "belongsToDomain",
		ObjectID:   domainConceptID,
		Direction:  models.RelationDirectionSubjectToObject,
		Confidence: confidence,
		Qualifiers: map[string]interface{}{
			"relevance": relevance, // "primary", "secondary", "tertiary"
		},
		Provenance: models.Provenance{
			SourceIDs:  []string{"domain_classifier"},
			ObservedAt: &now,
			Method:     "LLM_classification",
		},
	}

	_, err := m.relationsRepo.Create(ctx, relation)
	if err != nil {
		return fmt.Errorf("failed to create domain relation: %w", err)
	}

	if m.logger != nil {
		m.logger.Printf("[DomainRelationManager] Linked Entity %s to Domain %s (confidence: %.2f, relevance: %s)",
			entityID, domainConceptID, confidence, relevance)
	}

	return nil
}

// LinkEventToDomain создает Relation между Event и Domain Concept
func (m *DomainRelationManager) LinkEventToDomain(
	ctx context.Context,
	tenantID string,
	eventID string,
	domainConceptID string,
	confidence float64,
	relevance string,
) error {
	now := time.Now().UTC()

	relation := &models.Relation{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "system",
			UpdatedBy: "system",
			Labels:    map[string]string{"en": "belongsToDomain"},
			Status:    "active",
		},
		SubjectID:  eventID,
		Predicate:  "belongsToDomain",
		ObjectID:   domainConceptID,
		Direction:  models.RelationDirectionSubjectToObject,
		Confidence: confidence,
		Qualifiers: map[string]interface{}{
			"relevance": relevance,
		},
		Provenance: models.Provenance{
			SourceIDs:  []string{"domain_classifier"},
			ObservedAt: &now,
			Method:     "LLM_classification",
		},
	}

	_, err := m.relationsRepo.Create(ctx, relation)
	if err != nil {
		return fmt.Errorf("failed to create domain relation: %w", err)
	}

	if m.logger != nil {
		m.logger.Printf("[DomainRelationManager] Linked Event %s to Domain %s (confidence: %.2f, relevance: %s)",
			eventID, domainConceptID, confidence, relevance)
	}

	return nil
}

// LinkClaimToDomain создает Relation между Claim и Domain Concept
func (m *DomainRelationManager) LinkClaimToDomain(
	ctx context.Context,
	tenantID string,
	claimID string,
	domainConceptID string,
	confidence float64,
	relevance string,
) error {
	now := time.Now().UTC()

	relation := &models.Relation{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "system",
			UpdatedBy: "system",
			Labels:    map[string]string{"en": "belongsToDomain"},
			Status:    "active",
		},
		SubjectID:  claimID,
		Predicate:  "belongsToDomain",
		ObjectID:   domainConceptID,
		Direction:  models.RelationDirectionSubjectToObject,
		Confidence: confidence,
		Qualifiers: map[string]interface{}{
			"relevance": relevance,
		},
		Provenance: models.Provenance{
			SourceIDs:  []string{"domain_classifier"},
			ObservedAt: &now,
			Method:     "LLM_classification",
		},
	}

	_, err := m.relationsRepo.Create(ctx, relation)
	if err != nil {
		return fmt.Errorf("failed to create domain relation: %w", err)
	}

	if m.logger != nil {
		m.logger.Printf("[DomainRelationManager] Linked Claim %s to Domain %s (confidence: %.2f, relevance: %s)",
			claimID, domainConceptID, confidence, relevance)
	}

	return nil
}

// GetDomainsForObject возвращает все домены для объекта
func (m *DomainRelationManager) GetDomainsForObject(
	ctx context.Context,
	tenantID string,
	objectID string,
) ([]*DomainObjectRelation, error) {
	// Ищем все Relations с predicate "belongsToDomain" для данного объекта
	relations, err := m.relationsRepo.ListBySubject(ctx, tenantID, objectID, 100)
	if err != nil {
		return nil, fmt.Errorf("failed to list relations: %w", err)
	}

	var domainRelations []*DomainObjectRelation
	for _, rel := range relations {
		if rel.Predicate == "belongsToDomain" {
			relevance := "primary"
			if rel.Qualifiers != nil {
				if r, ok := rel.Qualifiers["relevance"].(string); ok {
					relevance = r
				}
			}

			domainRelations = append(domainRelations, &DomainObjectRelation{
				DomainConceptID: rel.ObjectID,
				Confidence:      rel.Confidence,
				Relevance:       relevance,
				RelationID:      rel.ID,
			})
		}
	}

	return domainRelations, nil
}

// GetObjectsForDomain возвращает все объекты для домена (с учетом иерархии)
func (m *DomainRelationManager) GetObjectsForDomain(
	ctx context.Context,
	tenantID string,
	domainConceptID string,
	includeDescendants bool,
	hierarchyService *DomainHierarchyService,
) ([]*DomainObject, error) {
	domainIDs := []string{domainConceptID}

	// Если нужно включить потомков, получаем их
	if includeDescendants {
		descendants, err := hierarchyService.GetAllDescendantDomains(ctx, tenantID, domainConceptID)
		if err == nil {
			for _, desc := range descendants {
				domainIDs = append(domainIDs, desc.ID)
			}
		}
	}

	var allObjects []*DomainObject

	// Для каждого домена ищем Relations
	for _, domainID := range domainIDs {
		relations, err := m.relationsRepo.ListByObject(ctx, tenantID, domainID, 1000)
		if err != nil {
			continue
		}

		for _, rel := range relations {
			if rel.Predicate == "belongsToDomain" {
				relevance := "primary"
				if rel.Qualifiers != nil {
					if r, ok := rel.Qualifiers["relevance"].(string); ok {
						relevance = r
					}
				}

				allObjects = append(allObjects, &DomainObject{
					ObjectID:       rel.SubjectID,
					DomainConceptID: domainID,
					Confidence:      rel.Confidence,
					Relevance:       relevance,
					RelationID:      rel.ID,
				})
			}
		}
	}

	return allObjects, nil
}

// DomainObjectRelation связь объекта с доменом
type DomainObjectRelation struct {
	DomainConceptID string  `json:"domainConceptId"`
	Confidence      float64 `json:"confidence"`
	Relevance       string  `json:"relevance"`
	RelationID      string  `json:"relationId"`
}

// DomainObject объект, связанный с доменом
type DomainObject struct {
	ObjectID        string  `json:"objectId"`
	DomainConceptID string  `json:"domainConceptId"`
	Confidence      float64 `json:"confidence"`
	Relevance       string  `json:"relevance"`
	RelationID      string  `json:"relationId"`
}


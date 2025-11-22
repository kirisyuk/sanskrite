package domain

import (
	"context"
	"fmt"
	"log"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DomainService главный сервис для работы с доменами
// Объединяет все компоненты и предоставляет единый интерфейс
type DomainService struct {
	hierarchyService *DomainHierarchyService
	classifier       *DomainClassifier
	relationManager  *DomainRelationManager
	logger           *log.Logger
}

// NewDomainService создает новый сервис доменов
func NewDomainService(
	conceptsRepo *repo.ConceptRepository,
	relationsRepo *repo.RelationRepository,
	genaiClient *gen_combined.Client,
	executor *ai.AITaskExecutor,
	logger *log.Logger,
) *DomainService {
	hierarchyService := NewDomainHierarchyService(conceptsRepo, logger)
	classifier := NewDomainClassifier(hierarchyService, conceptsRepo, genaiClient, executor, logger)
	relationManager := NewDomainRelationManager(relationsRepo, logger)

	return &DomainService{
		hierarchyService: hierarchyService,
		classifier:       classifier,
		relationManager:  relationManager,
		logger:           logger,
	}
}

// InitializeHierarchy инициализирует стандартную иерархию доменов
func (s *DomainService) InitializeHierarchy(ctx context.Context, tenantID string) error {
	return s.hierarchyService.InitializeDefaultHierarchy(ctx, tenantID)
}

// ClassifyAndLinkEntity классифицирует Entity и создает связи с доменами
func (s *DomainService) ClassifyAndLinkEntity(
	ctx context.Context,
	tenantID string,
	entity *models.Entity,
	contextText string,
) error {
	classifications, err := s.classifier.ClassifyEntity(ctx, tenantID, entity, contextText)
	if err != nil {
		return fmt.Errorf("failed to classify entity: %w", err)
	}

	// Создаем Relations для каждого домена
	for _, classification := range classifications {
		if err := s.relationManager.LinkEntityToDomain(
			ctx,
			tenantID,
			entity.ID,
			classification.DomainConceptID,
			classification.Confidence,
			classification.Relevance,
		); err != nil {
			if s.logger != nil {
				s.logger.Printf("[DomainService] Failed to link entity to domain: %v", err)
			}
			// Продолжаем обработку других доменов даже если один не удался
			continue
		}
	}

	if s.logger != nil {
		s.logger.Printf("[DomainService] Classified Entity %s to %d domains", entity.ID, len(classifications))
	}

	return nil
}

// ClassifyAndLinkEvent классифицирует Event и создает связи с доменами
func (s *DomainService) ClassifyAndLinkEvent(
	ctx context.Context,
	tenantID string,
	event *models.Event,
	contextText string,
) error {
	classifications, err := s.classifier.ClassifyEvent(ctx, tenantID, event, contextText)
	if err != nil {
		return fmt.Errorf("failed to classify event: %w", err)
	}

	// Создаем Relations для каждого домена
	for _, classification := range classifications {
		if err := s.relationManager.LinkEventToDomain(
			ctx,
			tenantID,
			event.ID,
			classification.DomainConceptID,
			classification.Confidence,
			classification.Relevance,
		); err != nil {
			if s.logger != nil {
				s.logger.Printf("[DomainService] Failed to link event to domain: %v", err)
			}
			continue
		}
	}

	if s.logger != nil {
		s.logger.Printf("[DomainService] Classified Event %s to %d domains", event.ID, len(classifications))
	}

	return nil
}

// ClassifyAndLinkClaim классифицирует Claim и создает связи с доменами
func (s *DomainService) ClassifyAndLinkClaim(
	ctx context.Context,
	tenantID string,
	claim *models.Claim,
	contextText string,
) error {
	classifications, err := s.classifier.ClassifyClaim(ctx, tenantID, claim, contextText)
	if err != nil {
		return fmt.Errorf("failed to classify claim: %w", err)
	}

	// Создаем Relations для каждого домена
	for _, classification := range classifications {
		if err := s.relationManager.LinkClaimToDomain(
			ctx,
			tenantID,
			claim.ID,
			classification.DomainConceptID,
			classification.Confidence,
			classification.Relevance,
		); err != nil {
			if s.logger != nil {
				s.logger.Printf("[DomainService] Failed to link claim to domain: %v", err)
			}
			continue
		}
	}

	if s.logger != nil {
		s.logger.Printf("[DomainService] Classified Claim %s to %d domains", claim.ID, len(classifications))
	}

	return nil
}

// GetDomainsForObject возвращает все домены для объекта
func (s *DomainService) GetDomainsForObject(
	ctx context.Context,
	tenantID string,
	objectID string,
) ([]*DomainObjectRelation, error) {
	return s.relationManager.GetDomainsForObject(ctx, tenantID, objectID)
}

// GetObjectsForDomain возвращает все объекты для домена
func (s *DomainService) GetObjectsForDomain(
	ctx context.Context,
	tenantID string,
	domainPath []string,
	includeDescendants bool,
) ([]*DomainObject, error) {
	// Получаем Concept для домена
	domainConcept, err := s.hierarchyService.GetDomainConcept(ctx, tenantID, domainPath)
	if err != nil {
		return nil, fmt.Errorf("domain not found: %w", err)
	}

	return s.relationManager.GetObjectsForDomain(
		ctx,
		tenantID,
		domainConcept.ID,
		includeDescendants,
		s.hierarchyService,
	)
}

// GetDomainPath возвращает путь домена по Concept ID
func (s *DomainService) GetDomainPath(ctx context.Context, domainConceptID string) ([]string, error) {
	return s.hierarchyService.GetDomainPath(ctx, domainConceptID)
}


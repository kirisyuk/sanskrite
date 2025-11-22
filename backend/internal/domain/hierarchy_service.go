package domain

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DomainHierarchyService управляет иерархией доменов
type DomainHierarchyService struct {
	conceptsRepo *repo.ConceptRepository
	logger       *log.Logger
	// Кэш для быстрого доступа к Concepts по пути
	pathCache map[string]string // "Economic/Financial" -> concept_id
}

// NewDomainHierarchyService создает новый сервис иерархии доменов
func NewDomainHierarchyService(
	conceptsRepo *repo.ConceptRepository,
	logger *log.Logger,
) *DomainHierarchyService {
	return &DomainHierarchyService{
		conceptsRepo: conceptsRepo,
		logger:       logger,
		pathCache:    make(map[string]string),
	}
}

// InitializeDefaultHierarchy создает стандартную 4-уровневую иерархию доменов
func (s *DomainHierarchyService) InitializeDefaultHierarchy(
	ctx context.Context,
	tenantID string,
) error {
	if s.logger != nil {
		s.logger.Printf("[DomainHierarchy] Initializing default hierarchy for tenant: %s", tenantID)
	}

	now := time.Now().UTC()
	createdBy := "system"

	// Рекурсивно создаем Concepts для каждого узла
	err := s.createDomainConceptsRecursive(
		ctx,
		tenantID,
		StandardDomainHierarchy,
		nil, // root не имеет родителя
		[]string{}, // root path пустой
		now,
		createdBy,
	)

	if err != nil {
		return fmt.Errorf("failed to initialize domain hierarchy: %w", err)
	}

	if s.logger != nil {
		s.logger.Printf("[DomainHierarchy] Default hierarchy initialized successfully")
	}

	return nil
}

// createDomainConceptsRecursive рекурсивно создает Concepts для узлов иерархии
func (s *DomainHierarchyService) createDomainConceptsRecursive(
	ctx context.Context,
	tenantID string,
	node *DomainHierarchyNode,
	parentID *string,
	path []string,
	now time.Time,
	createdBy string,
) error {
	// Создаем текущий Concept
	concept := &models.Concept{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: createdBy,
			UpdatedBy: createdBy,
			Labels:    map[string]string{"en": node.Name},
			Status:    "active",
		},
		Name: node.Name,
		Kind: models.ConceptKindDomain,
		Hierarchy: models.ConceptHierarchy{
			ParentID: parentID,
			Path:     path,
		},
	}

	// Проверяем, существует ли уже такой Concept
	// Ищем по tenantID + name + kind=Domain
	existingConcepts, err := s.conceptsRepo.ListByTenantAndKind(ctx, tenantID, models.ConceptKindDomain, 1000)
	if err == nil {
		for _, existing := range existingConcepts {
			if existing.Name == node.Name && existing.Hierarchy.ParentID != nil && parentID != nil && *existing.Hierarchy.ParentID == *parentID {
				// Concept уже существует, используем его ID
				concept.ID = existing.ID
				if s.logger != nil {
					s.logger.Printf("[DomainHierarchy] Concept already exists: %s (ID: %s)", node.Name, existing.ID)
				}
				break
			} else if existing.Name == node.Name && existing.Hierarchy.ParentID == nil && parentID == nil {
				// Root level concept
				concept.ID = existing.ID
				if s.logger != nil {
					s.logger.Printf("[DomainHierarchy] Root concept already exists: %s (ID: %s)", node.Name, existing.ID)
				}
				break
			}
		}
	}

	// Если Concept не существует, создаем его
	if concept.ID == "" {
		conceptID, err := s.conceptsRepo.Create(ctx, concept)
		if err != nil {
			return fmt.Errorf("failed to create concept %s: %w", node.Name, err)
		}
		concept.ID = conceptID
		if s.logger != nil {
			s.logger.Printf("[DomainHierarchy] Created concept: %s (ID: %s)", node.Name, conceptID)
		}
	} else {
		// Обновляем существующий Concept (на случай изменения иерархии)
		concept.UpdatedAt = now
		concept.UpdatedBy = createdBy
		if err := s.conceptsRepo.Upsert(ctx, concept); err != nil {
			return fmt.Errorf("failed to update concept %s: %w", node.Name, err)
		}
	}

	// Кэшируем путь
	pathKey := s.buildPathKey(path, node.Name)
	s.pathCache[pathKey] = concept.ID

	// Рекурсивно создаем дочерние Concepts
	newPath := append(path, concept.ID)
	for _, child := range node.Children {
		if err := s.createDomainConceptsRecursive(
			ctx,
			tenantID,
			child,
			&concept.ID,
			newPath,
			now,
			createdBy,
		); err != nil {
			return err
		}
	}

	return nil
}

// buildPathKey создает ключ для кэша пути
func (s *DomainHierarchyService) buildPathKey(path []string, name string) string {
	key := ""
	for _, p := range path {
		key += p + "/"
	}
	key += name
	return key
}

// GetDomainPath возвращает полный путь домена (например, ["Economic", "Financial", "Banking"])
func (s *DomainHierarchyService) GetDomainPath(
	ctx context.Context,
	domainConceptID string,
) ([]string, error) {
	concept, err := s.conceptsRepo.Get(ctx, domainConceptID)
	if err != nil {
		return nil, fmt.Errorf("failed to get concept: %w", err)
	}

	if concept.Kind != models.ConceptKindDomain {
		return nil, fmt.Errorf("concept %s is not a domain", domainConceptID)
	}

	// Строим путь от корня до текущего узла
	path := []string{concept.Name}
	currentID := concept.Hierarchy.ParentID

	for currentID != nil {
		parent, err := s.conceptsRepo.Get(ctx, *currentID)
		if err != nil {
			break
		}
		path = append([]string{parent.Name}, path...)
		currentID = parent.Hierarchy.ParentID
	}

	return path, nil
}

// GetDomainConcept возвращает Concept для домена по пути
func (s *DomainHierarchyService) GetDomainConcept(
	ctx context.Context,
	tenantID string,
	domainPath []string,
) (*models.Concept, error) {
	if len(domainPath) == 0 {
		return nil, fmt.Errorf("domain path cannot be empty")
	}

	// Проверяем кэш
	pathKey := ""
	for _, part := range domainPath {
		if pathKey != "" {
			pathKey += "/"
		}
		pathKey += part
	}

	if cachedID, ok := s.pathCache[pathKey]; ok {
		concept, err := s.conceptsRepo.Get(ctx, cachedID)
		if err == nil {
			return concept, nil
		}
	}

	// Если не в кэше, ищем рекурсивно
	allConcepts, err := s.conceptsRepo.ListByTenantAndKind(ctx, tenantID, models.ConceptKindDomain, 1000)
	if err != nil {
		return nil, fmt.Errorf("failed to list concepts: %w", err)
	}

	// Находим корневой Concept (первый элемент пути)
	var currentConcept *models.Concept
	for _, c := range allConcepts {
		if c.Name == domainPath[0] && c.Hierarchy.ParentID == nil {
			currentConcept = c
			break
		}
	}

	if currentConcept == nil {
		return nil, fmt.Errorf("domain not found: %s", domainPath[0])
	}

	// Идем по пути
	for i := 1; i < len(domainPath); i++ {
		found := false
		for _, c := range allConcepts {
			if c.Name == domainPath[i] && c.Hierarchy.ParentID != nil && *c.Hierarchy.ParentID == currentConcept.ID {
				currentConcept = c
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("domain path not found: %v", domainPath)
		}
	}

	// Кэшируем найденный путь
	s.pathCache[pathKey] = currentConcept.ID

	return currentConcept, nil
}

// GetChildDomains возвращает все дочерние домены для заданного домена
func (s *DomainHierarchyService) GetChildDomains(
	ctx context.Context,
	tenantID string,
	parentDomainID string,
) ([]*models.Concept, error) {
	allConcepts, err := s.conceptsRepo.ListByTenantAndKind(ctx, tenantID, models.ConceptKindDomain, 1000)
	if err != nil {
		return nil, fmt.Errorf("failed to list concepts: %w", err)
	}

	var children []*models.Concept
	for _, c := range allConcepts {
		if c.Hierarchy.ParentID != nil && *c.Hierarchy.ParentID == parentDomainID {
			children = append(children, c)
		}
	}

	return children, nil
}

// GetAllDescendantDomains возвращает все потомки домена (рекурсивно)
func (s *DomainHierarchyService) GetAllDescendantDomains(
	ctx context.Context,
	tenantID string,
	parentDomainID string,
) ([]*models.Concept, error) {
	var descendants []*models.Concept

	children, err := s.GetChildDomains(ctx, tenantID, parentDomainID)
	if err != nil {
		return nil, err
	}

	for _, child := range children {
		descendants = append(descendants, child)
		// Рекурсивно получаем потомков
		grandchildren, err := s.GetAllDescendantDomains(ctx, tenantID, child.ID)
		if err != nil {
			return nil, err
		}
		descendants = append(descendants, grandchildren...)
	}

	return descendants, nil
}


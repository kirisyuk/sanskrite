package domain

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DomainCache продвинутый кэш для классификаций доменов
type DomainCache struct {
	// In-memory cache с TTL
	memoryCache map[string]*CachedClassification
	cacheMutex  sync.RWMutex
	ttl         time.Duration

	// Персистентный кэш (опционально, через Firestore)
	persistentCache *repo.ProcessedContentRepository
	logger          *log.Logger
}

// NewDomainCache создает новый кэш доменов
func NewDomainCache(
	ttl time.Duration,
	persistentCache *repo.ProcessedContentRepository,
	logger *log.Logger,
) *DomainCache {
	return &DomainCache{
		memoryCache:     make(map[string]*CachedClassification),
		ttl:             ttl,
		persistentCache: persistentCache,
		logger:          logger,
	}
}

// Get получает классификацию из кэша
func (c *DomainCache) Get(ctx context.Context, key string) (*CachedClassification, bool) {
	c.cacheMutex.RLock()
	defer c.cacheMutex.RUnlock()

	// Проверяем in-memory cache
	if cached, ok := c.memoryCache[key]; ok {
		// Проверяем TTL
		if time.Now().Unix()-cached.CachedAt < int64(c.ttl.Seconds()) {
			return cached, true
		}
		// TTL истек, удаляем из кэша
		delete(c.memoryCache, key)
	}

	// Проверяем персистентный кэш
	// ProcessedContentRepository предназначен для SourceProfileData, не подходит для классификаций
	// Для персистентного кэша классификаций лучше использовать отдельную коллекцию Firestore
	// Пока используем только in-memory кэш

	return nil, false
}

// Set сохраняет классификацию в кэш
func (c *DomainCache) Set(ctx context.Context, key string, classification *CachedClassification) {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	// Обновляем timestamp
	classification.CachedAt = time.Now().Unix()

	// Сохраняем в memory cache
	c.memoryCache[key] = classification

	// Сохраняем в persistent cache (если доступен)
	// ProcessedContentRepository предназначен для SourceProfileData, не подходит для классификаций
	// Для персистентного кэша классификаций лучше использовать отдельную коллекцию Firestore
	// Пока используем только in-memory кэш с TTL и LRU eviction

	// LRU eviction: если кэш слишком большой, удаляем старые записи
	if len(c.memoryCache) > 1000 {
		c.evictOldest()
	}
}

// Invalidate инвалидирует кэш для объекта
func (c *DomainCache) Invalidate(ctx context.Context, objectID string) {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	// Удаляем все записи, связанные с объектом
	for key := range c.memoryCache {
		// Предполагаем формат ключа: "Entity:objectID" или "Event:objectID"
		if strings.Contains(key, objectID) {
			delete(c.memoryCache, key)
		}
	}
}

// evictOldest удаляет самые старые записи из кэша (LRU)
func (c *DomainCache) evictOldest() {
	// Находим самую старую запись
	var oldestKey string
	var oldestTime int64 = time.Now().Unix()

	for key, cached := range c.memoryCache {
		if cached.CachedAt < oldestTime {
			oldestTime = cached.CachedAt
			oldestKey = key
		}
	}

	// Удаляем самую старую запись
	if oldestKey != "" {
		delete(c.memoryCache, oldestKey)
		if c.logger != nil {
			c.logger.Printf("[DomainCache] Evicted oldest entry: %s", oldestKey)
		}
	}
}

// Clear очищает весь кэш
func (c *DomainCache) Clear() {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	c.memoryCache = make(map[string]*CachedClassification)
}


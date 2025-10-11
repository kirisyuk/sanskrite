package ai

// BaseAIService базовый абстрактный сервис для работы с разными AI-провайдерами
// Конкретные реализации (OpenAIService, VertexAIService и т.д.) встраивают эту структуру
type BaseAIService struct {
	// ModelManager менеджер моделей, содержащий их характеристики и цены
	ModelManager *AIModelManager
}

// NewBaseAIService создает новый экземпляр базового AI-сервиса
func NewBaseAIService(modelManager *AIModelManager) *BaseAIService {
	return &BaseAIService{
		ModelManager: modelManager,
	}
}


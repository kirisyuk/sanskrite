package scout

import (
	"strings"
)

// InferTaxonomyFromCanonicalPath определяет entityPath и sectorPath из canonicalPath
// Использует эвристики на основе структуры canonicalPath согласно Core Taxonomy v1.0
func InferTaxonomyFromCanonicalPath(canonicalPath string) (entityPath, sectorPath string) {
	// Разбираем canonicalPath на части
	parts := strings.Split(strings.Trim(canonicalPath, "/"), "/")
	if len(parts) == 0 {
		return getDefaultEntityPath(), getDefaultSectorPath()
	}

	// Определяем тип по первому сегменту
	firstSegment := strings.ToLower(parts[0])

	switch firstSegment {
	case "org", "organization", "company", "corp", "corp.":
		// Организация
		entityPath = inferOrganizationPath(parts)
		sectorPath = inferSectorFromPath(parts)
	case "infra", "infrastructure", "facility", "fab", "plant", "factory":
		// Инфраструктура
		entityPath = inferInfrastructurePath(parts)
		sectorPath = inferSectorFromPath(parts)
	case "geo", "geography", "region", "country", "location":
		// Географический регион
		entityPath = "/Geo/Region"
		sectorPath = "/Geography"
	case "resource", "material", "commodity":
		// Ресурс
		entityPath = inferResourcePath(parts)
		sectorPath = inferSectorFromPath(parts)
	case "person", "people":
		// Персона
		entityPath = "/Person/Individual"
		sectorPath = inferSectorFromPath(parts)
	case "event":
		// Событие
		entityPath = "/Event"
		sectorPath = inferSectorFromPath(parts)
	default:
		// Пытаемся определить по содержимому пути
		entityPath = inferEntityPathFromContent(parts)
		sectorPath = inferSectorFromContent(parts)
	}

	// Если не удалось определить, используем значения по умолчанию
	if entityPath == "" {
		entityPath = getDefaultEntityPath()
	}
	if sectorPath == "" {
		sectorPath = getDefaultSectorPath()
	}

	return entityPath, sectorPath
}

// inferOrganizationPath определяет путь организации
func inferOrganizationPath(parts []string) string {
	if len(parts) < 2 {
		return "/Organization/Corporation/Public"
	}

	// Проверяем второй сегмент
	secondSegment := strings.ToLower(parts[1])
	
	switch secondSegment {
	case "gov", "government", "state":
		return "/Organization/Government/Agency"
	case "military", "defense":
		return "/Organization/Government/Military"
	case "ngo", "nonprofit":
		return "/Organization/NonProfit/NGO"
	case "international", "igo":
		return "/Organization/International/IGO"
	case "private":
		return "/Organization/Corporation/Private"
	case "soe", "state-owned":
		return "/Organization/Corporation/StateOwned"
	default:
		return "/Organization/Corporation/Public"
	}
}

// inferInfrastructurePath определяет путь инфраструктуры
func inferInfrastructurePath(parts []string) string {
	if len(parts) < 2 {
		return "/Infrastructure/Manufacturing/Factory"
	}

	secondSegment := strings.ToLower(parts[1])
	
	switch secondSegment {
	case "logistics", "port", "airport", "rail":
		return "/Infrastructure/Logistics/Seaport"
	case "energy", "power", "plant":
		if len(parts) >= 3 {
			thirdSegment := strings.ToLower(parts[2])
			switch thirdSegment {
			case "nuclear":
				return "/Infrastructure/Energy/PowerPlant/Nuclear"
			case "thermal", "coal", "gas":
				return "/Infrastructure/Energy/PowerPlant/Thermal"
			case "renewable", "solar", "wind":
				return "/Infrastructure/Energy/PowerPlant/Renewable"
			case "grid", "substation":
				return "/Infrastructure/Energy/Grid/Substation"
			case "pipeline":
				return "/Infrastructure/Energy/Pipeline/Oil"
			}
		}
		return "/Infrastructure/Energy/PowerPlant/Thermal"
	case "manufacturing", "factory", "fab", "plant":
		return "/Infrastructure/Manufacturing/Factory"
	case "mine", "mining":
		return "/Infrastructure/Manufacturing/Mine"
	case "digital", "datacenter", "data-center":
		return "/Infrastructure/Digital/DataCenter"
	case "internet", "ix", "exchange":
		return "/Infrastructure/Digital/InternetExchange"
	default:
		return "/Infrastructure/Manufacturing/Factory"
	}
}

// inferResourcePath определяет путь ресурса
func inferResourcePath(parts []string) string {
	if len(parts) < 2 {
		return "/Resource/RawMaterial/Mineral"
	}

	secondSegment := strings.ToLower(parts[1])
	
	switch secondSegment {
	case "mineral", "mining", "ore":
		return "/Resource/RawMaterial/Mineral"
	case "agricultural", "agriculture", "crop":
		return "/Resource/RawMaterial/Agricultural"
	case "energy", "fuel", "oil", "gas":
		return "/Resource/Energy/Fossil"
	case "electricity", "power":
		return "/Resource/Energy/Electricity"
	default:
		return "/Resource/RawMaterial/Mineral"
	}
}

// inferSectorFromPath определяет отраслевой путь из сегментов пути
func inferSectorFromPath(parts []string) string {
	// Ищем ключевые слова в пути
	pathStr := strings.ToLower(strings.Join(parts, "/"))
	
	// Технологии
	if strings.Contains(pathStr, "tech") || strings.Contains(pathStr, "semiconductor") || 
	   strings.Contains(pathStr, "chip") || strings.Contains(pathStr, "fab") {
		return "/Technology/Semiconductors/Manufacturing"
	}
	
	// Энергетика
	if strings.Contains(pathStr, "energy") || strings.Contains(pathStr, "power") || 
	   strings.Contains(pathStr, "oil") || strings.Contains(pathStr, "gas") {
		return "/Energy/Fossil"
	}
	
	// Материалы
	if strings.Contains(pathStr, "material") || strings.Contains(pathStr, "mining") {
		return "/Materials/Metals"
	}
	
	// Финансы
	if strings.Contains(pathStr, "finance") || strings.Contains(pathStr, "bank") {
		return "/Finance/Banking"
	}
	
	// По умолчанию
	return getDefaultSectorPath()
}

// inferEntityPathFromContent пытается определить entityPath по содержимому
func inferEntityPathFromContent(parts []string) string {
	pathStr := strings.ToLower(strings.Join(parts, "/"))
	
	if strings.Contains(pathStr, "org") || strings.Contains(pathStr, "company") {
		return "/Organization/Corporation/Public"
	}
	if strings.Contains(pathStr, "facility") || strings.Contains(pathStr, "factory") || 
	   strings.Contains(pathStr, "plant") || strings.Contains(pathStr, "fab") {
		return "/Infrastructure/Manufacturing/Factory"
	}
	if strings.Contains(pathStr, "geo") || strings.Contains(pathStr, "region") {
		return "/Geo/Region"
	}
	
	return ""
}

// inferSectorFromContent пытается определить sectorPath по содержимому
func inferSectorFromContent(parts []string) string {
	return inferSectorFromPath(parts)
}

// getDefaultEntityPath возвращает путь по умолчанию
func getDefaultEntityPath() string {
	return "/Organization/Corporation/Public"
}

// getDefaultSectorPath возвращает отраслевой путь по умолчанию
func getDefaultSectorPath() string {
	return "/Technology/Semiconductors/Manufacturing"
}


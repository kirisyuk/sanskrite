package domain

// DomainClassification результат классификации объекта к домену
type DomainClassification struct {
	DomainConceptID string   `json:"domainConceptId"` // ID Concept домена
	DomainPath      []string `json:"domainPath"`       // ["Economic", "Financial", "Banking"]
	Confidence      float64  `json:"confidence"`       // 0..1
	Relevance       string   `json:"relevance"`       // "primary", "secondary", "tertiary"
	Reasoning       string   `json:"reasoning"`        // LLM reasoning
}

// DomainHierarchyNode узел иерархии доменов для инициализации
type DomainHierarchyNode struct {
	Name     string                `json:"name"`
	Children []*DomainHierarchyNode `json:"children,omitempty"`
}

// StandardDomainHierarchy стандартная 4-уровневая иерархия доменов
var StandardDomainHierarchy = &DomainHierarchyNode{
	Name: "BusinessDomain",
	Children: []*DomainHierarchyNode{
		{
			Name: "Economic",
			Children: []*DomainHierarchyNode{
				{
					Name: "Financial",
					Children: []*DomainHierarchyNode{
						{Name: "Banking"},
						{Name: "Investment"},
						{Name: "Insurance"},
						{Name: "Cryptocurrency"},
					},
				},
				{
					Name: "Market",
					Children: []*DomainHierarchyNode{
						{Name: "StockMarket"},
						{Name: "CommodityMarket"},
						{Name: "RealEstateMarket"},
					},
				},
				{
					Name: "Trade",
					Children: []*DomainHierarchyNode{
						{Name: "InternationalTrade"},
						{Name: "ECommerce"},
						{Name: "SupplyChain"},
					},
				},
			},
		},
		{
			Name: "Political",
			Children: []*DomainHierarchyNode{
				{Name: "Government"},
				{Name: "Policy"},
				{Name: "Regulation"},
				{Name: "InternationalRelations"},
			},
		},
		{
			Name: "Social",
			Children: []*DomainHierarchyNode{
				{Name: "Demographics"},
				{Name: "Culture"},
				{Name: "Education"},
				{Name: "Healthcare"},
			},
		},
		{
			Name: "Technical",
			Children: []*DomainHierarchyNode{
				{
					Name: "Technology",
					Children: []*DomainHierarchyNode{
						{Name: "AI"},
						{Name: "CloudComputing"},
						{Name: "Cybersecurity"},
						{Name: "Blockchain"},
					},
				},
				{Name: "Infrastructure"},
				{Name: "Research"},
			},
		},
		{
			Name: "Environmental",
			Children: []*DomainHierarchyNode{
				{Name: "Climate"},
				{Name: "Energy"},
				{Name: "Sustainability"},
			},
		},
		{
			Name: "Legal",
			Children: []*DomainHierarchyNode{
				{Name: "CorporateLaw"},
				{Name: "IntellectualProperty"},
				{Name: "Compliance"},
			},
		},
	},
}

// DomainCacheKey ключ для кэша классификации
type DomainCacheKey struct {
	ObjectID   string
	ObjectKind string
	ObjectHash string // хэш объекта для инвалидации кэша
}

// CachedClassification закэшированная классификация
type CachedClassification struct {
	Classifications []*DomainClassification
	CachedAt        int64 // Unix timestamp
}


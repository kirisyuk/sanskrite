package config

import (
	"embed"
	"fmt"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed materials/*.yaml
var materialsFS embed.FS

// MaterialDefinition matches whitepaper 7.2.
type MaterialDefinition struct {
	MaterialID    string            `yaml:"material_id"`
	DisplayName   map[string]string  `yaml:"display_name"`
	ChemicalSymbol string           `yaml:"chemical_symbol,omitempty"`
	Category     string             `yaml:"category"`
	Subcategory  string             `yaml:"subcategory,omitempty"`
	Units        *MaterialUnits     `yaml:"units,omitempty"`
	ProductionStages []ProductionStage `yaml:"production_stages"`
	DataSources  []DataSourceConfig `yaml:"data_sources,omitempty"`
	KeyChokepoints []ChokepointConfig `yaml:"key_chokepoints,omitempty"`
	ConcentrationHHI float64         `yaml:"concentration_hhi,omitempty"`
}

type MaterialUnits struct {
	Primary string   `yaml:"primary"`
	Aliases []string `yaml:"aliases,omitempty"`
}

type ProductionStage struct {
	ID             int      `yaml:"id"`
	Name           string   `yaml:"name"`
	Type           string   `yaml:"type"`
	FacilityKind   string   `yaml:"facilityKind"`
	TypicalForms   []string `yaml:"typical_forms,omitempty"`
	ConversionRatio float64 `yaml:"conversion_ratio,omitempty"`
}

type DataSourceConfig struct {
	SourceID string   `yaml:"source_id"`
	Type     string   `yaml:"type"`
	Fields   []string `yaml:"fields,omitempty"`
}

type ChokepointConfig struct {
	Path                 string  `yaml:"path"`
	ShareOfGlobalSupply  float64 `yaml:"share_of_global_supply"`
	RiskMultiplier       float64 `yaml:"risk_multiplier"`
}

var (
	configCache map[string]*MaterialDefinition
	configMu    sync.RWMutex
)

// LoadMaterial loads a material definition by ID from embedded YAML.
func LoadMaterial(materialID string) (*MaterialDefinition, error) {
	configMu.RLock()
	if c, ok := configCache[materialID]; ok {
		configMu.RUnlock()
		return c, nil
	}
	configMu.RUnlock()

	configMu.Lock()
	defer configMu.Unlock()
	if configCache == nil {
		configCache = make(map[string]*MaterialDefinition)
	}
	if c, ok := configCache[materialID]; ok {
		return c, nil
	}

	path := "materials/" + materialID + ".yaml"
	data, err := materialsFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read material config %s: %w", materialID, err)
	}
	var def MaterialDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse material config %s: %w", materialID, err)
	}
	def.MaterialID = materialID
	configCache[materialID] = &def
	return &def, nil
}

// ListMaterialIDs returns material IDs from embedded configs.
func ListMaterialIDs() ([]string, error) {
	entries, err := materialsFS.ReadDir("materials")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > 5 && e.Name()[len(e.Name())-5:] == ".yaml" {
			ids = append(ids, e.Name()[:len(e.Name())-5])
		}
	}
	return ids, nil
}

// LoadAllMaterials loads all material definitions.
func LoadAllMaterials() ([]*MaterialDefinition, error) {
	ids, err := ListMaterialIDs()
	if err != nil {
		return nil, err
	}
	var out []*MaterialDefinition
	for _, id := range ids {
		m, err := LoadMaterial(id)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

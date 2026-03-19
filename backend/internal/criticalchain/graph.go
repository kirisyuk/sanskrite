package criticalchain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/config"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
)

func buildGraphMaterialsAndSteps(material *models.Material, def *config.MaterialDefinition, facilities []*models.Facility, stored []*models.ProcessingStep) ([]*models.Material, []*models.ProcessingStep) {
	if material == nil {
		return nil, stored
	}

	materials := []*models.Material{material}
	stageNodes := make(map[int]*models.Material)
	stageDefs := make(map[int]config.ProductionStage)
	if def != nil {
		stages := append([]config.ProductionStage(nil), def.ProductionStages...)
		sort.Slice(stages, func(i, j int) bool { return stages[i].ID < stages[j].ID })
		for _, stage := range stages {
			stageNode := &models.Material{
				ID:               stageNodeID(def.MaterialID, stage.ID),
				Name:             fmt.Sprintf("%s / %s", material.Name, humanizeStageName(stage.Name)),
				Category:         material.Category,
				CriticalityScore: material.CriticalityScore,
				RiskScore:        material.RiskScore,
				UpdatedAt:        material.UpdatedAt,
			}
			stageNodes[stage.ID] = stageNode
			stageDefs[stage.ID] = stage
			materials = append(materials, stageNode)
		}
	}

	stepKey := make(map[string]bool)
	var out []*models.ProcessingStep
	addStep := func(step *models.ProcessingStep) {
		if step == nil {
			return
		}
		key := fmt.Sprintf("%s|%s|%s", step.From, step.To, step.Type)
		if stepKey[key] {
			return
		}
		stepKey[key] = true
		out = append(out, step)
	}

	if def != nil && len(def.ProductionStages) > 0 {
		sortedStages := append([]config.ProductionStage(nil), def.ProductionStages...)
		sort.Slice(sortedStages, func(i, j int) bool { return sortedStages[i].ID < sortedStages[j].ID })
		prev := material.ID
		for _, stage := range sortedStages {
			node := stageNodes[stage.ID]
			if node == nil {
				continue
			}
			addStep(&models.ProcessingStep{
				From: prev,
				To:   node.ID,
				Type: processingStepType(stage.Type),
			})
			prev = node.ID
		}
	}

	for _, facility := range facilities {
		from := material.ID
		stepType := models.StepProcessing
		if stage, ok := stageDefs[facility.Stage]; ok {
			from = stageNodeID(material.ID, facility.Stage)
			stepType = processingStepType(stage.Type)
		}
		addStep(&models.ProcessingStep{
			From: from,
			To:   facility.ID,
			Type: stepType,
		})
	}

	validNodes := make(map[string]bool, len(materials)+len(facilities))
	for _, node := range materials {
		validNodes[node.ID] = true
	}
	for _, facility := range facilities {
		validNodes[facility.ID] = true
	}
	for _, step := range stored {
		if step == nil {
			continue
		}
		if validNodes[step.From] && validNodes[step.To] {
			addStep(step)
		}
	}

	return materials, out
}

func stageNodeID(materialID string, stageID int) string {
	return fmt.Sprintf("%s:stage:%d", materialID, stageID)
}

func humanizeStageName(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.TrimSpace(name)
	if name == "" {
		return "Stage"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func processingStepType(kind string) models.ProcessingStepType {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "extraction":
		return models.StepExtraction
	case "refining":
		return models.StepRefining
	case "manufacturing":
		return models.StepManufacturing
	case "transport", "logistics", "distribution":
		return models.StepTransport
	default:
		return models.StepProcessing
	}
}

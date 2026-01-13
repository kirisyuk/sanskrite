package main

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func bestLabel(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if s := strings.TrimSpace(f.Labels["en"]); s != "" {
		return s
	}
	if s := strings.TrimSpace(f.Identifiers["legalName"]); s != "" {
		return s
	}
	if s := strings.TrimSpace(f.Identifiers["shortName"]); s != "" {
		return s
	}
	return ""
}

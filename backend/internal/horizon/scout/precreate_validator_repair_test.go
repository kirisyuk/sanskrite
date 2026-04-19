package scout

import (
	"context"
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestRepairInvalidPath_FinancialProductNotGeneric(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil)

	frag := &models.HorizonFragment{
		Class:          FragmentTypeProduct,
		CanonicalPath: "/product/x",
		Labels:        map[string]string{"en": "Corporate Loan"},
		Identifiers:   map[string]string{},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil result")
	}

	cp := strings.TrimSpace(frag.CanonicalPath)
	if !strings.HasPrefix(cp, "/product/service/financial/") {
		t.Fatalf("financial product should repair to product/service/financial/..., got %q", cp)
	}
	if strings.Contains(cp, "/product/finished/mechanical/assembly/") {
		t.Fatalf("financial product must NOT be repaired to generic mechanical/assembly path, got %q", cp)
	}
}

func TestRepairInvalidPath_NonFinancialProductUsesGeneric(t *testing.T) {
	v := NewPreCreateValidator(nil, nil, nil)

	frag := &models.HorizonFragment{
		Class:          FragmentTypeProduct,
		CanonicalPath: "/product/x",
		Labels:        map[string]string{"en": "Lithium Battery"},
		Identifiers:   map[string]string{},
	}

	res, err := v.ValidateAndProcess(context.Background(), frag)
	if err != nil {
		t.Fatalf("ValidateAndProcess() error = %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil result")
	}

	cp := strings.TrimSpace(frag.CanonicalPath)
	if !strings.HasPrefix(cp, "/product/finished/mechanical/assembly/") {
		t.Fatalf("non-financial product should repair to product/finished/mechanical/assembly/..., got %q", cp)
	}
}

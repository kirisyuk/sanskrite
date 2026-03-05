package agents

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestResolveResearchProfile(t *testing.T) {
	tests := []struct {
		name          string
		fragmentType  models.FragmentType
		canonicalPath string
		communityType string
		want          ResearchProfile
	}{
		{"market", models.FragmentTypeMarket, "/market/us/finance/banking", "", ResearchProfileMarket},
		{"product", models.FragmentTypeProduct, "/product/raw/mineral/extraction/lithium", "", ResearchProfileProduct},
		{"facility", models.FragmentTypeFacility, "/fac/cn/gigafactory-shanghai", "", ResearchProfileFacility},
		{"deposit", models.FragmentTypeDeposit, "/deposit/lithium/atacama-salar", "", ResearchProfileDeposit},
		{"gov", models.FragmentTypeOrganization, "/gov/us/sec", "", ResearchProfileGov},
		{"dynasty", models.FragmentTypeCommunity, "/community/sa/dynasty/house-of-saud", "dynasty", ResearchProfileCommunityDynastyTribe},
		{"domain", models.FragmentTypeDomain, "/domain/v1/supply-chain/geo/us", "", ResearchProfileDomain},
		{"general", models.FragmentTypeOrganization, "/org/us/tesla-inc", "", ResearchProfileGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveResearchProfile(tt.fragmentType, tt.canonicalPath, tt.communityType)
			if got != tt.want {
				t.Fatalf("ResolveResearchProfile() = %v, want %v", got, tt.want)
			}
		})
	}
}

package agents

import (
	"testing"
)

func TestResolveResearchProfile(t *testing.T) {
	tests := []struct {
		name          string
		fragmentType  FragmentType
		canonicalPath string
		communityType string
		want          ResearchProfile
	}{
		{"market", FragmentTypeMarket, "/market/us/finance/banking", "", ResearchProfileMarket},
		{"product", FragmentTypeProduct, "/product/raw/mineral/extraction/lithium", "", ResearchProfileProduct},
		{"facility", FragmentTypeFacility, "/fac/cn/gigafactory-shanghai", "", ResearchProfileFacility},
		{"deposit", FragmentTypeFacility, "/deposit/lithium/atacama-salar", "", ResearchProfileDeposit},
		{"gov", FragmentTypeOrganization, "/gov/us/sec", "", ResearchProfileGov},
		{"dynasty", FragmentTypeCommunity, "/community/sa/dynasty/house-of-saud", "dynasty", ResearchProfileCommunityDynastyTribe},
		{"domain", "", "/domain/v1/supply-chain/geo/us", "", ResearchProfileDomain},
		{"general", FragmentTypeOrganization, "/org/us/tesla-inc", "", ResearchProfileGeneral},
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

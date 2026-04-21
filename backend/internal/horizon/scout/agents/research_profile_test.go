package agents

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"testing"
)

func TestResolveResearchProfile(t *testing.T) {
	tests := []struct {
		name          string
		fragmentType  ah.FragmentClass
		canonicalPath string
		communityType string
		want          ResearchProfile
	}{
		{"market", ah.FragmentClassMarket, "/market/us/finance/banking", "", ResearchProfileMarket},
		{"product", ah.FragmentClassProduct, "/product/raw/mineral/extraction/lithium", "", ResearchProfileProduct},
		{"facility", ah.FragmentClassSite, "/fac/cn/gigafactory-shanghai", "", ResearchProfileFacility},
		{"deposit", ah.FragmentClassSite, "/deposit/lithium/atacama-salar", "", ResearchProfileDeposit},
		{"gov", ah.FragmentClassOrg, "/gov/us/sec", "", ResearchProfileGov},
		{"dynasty", ah.FragmentClassCommunity, "/community/sa/dynasty/house-of-saud", "dynasty", ResearchProfileCommunityDynastyTribe},
		{"domain", "", "/domain/v1/supply-chain/geo/us", "", ResearchProfileDomain},
		{"general", ah.FragmentClassOrg, "/org/us/tesla-inc", "", ResearchProfileGeneral},
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

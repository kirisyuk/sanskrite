package focusobject

import (
	"encoding/json"
	"fmt"
	"os"
)

func LoadFixture(path string) (*Fixture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture: %w", err)
	}
	var fixture Fixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		return nil, fmt.Errorf("decode fixture: %w", err)
	}
	return &fixture, nil
}

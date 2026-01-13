package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func readCursorFile(path string) (*cursorState, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("empty cursor file path")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st cursorState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func writeCursorFile(path string, st *cursorState) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty cursor file path")
	}
	if st == nil {
		return fmt.Errorf("nil cursor state")
	}
	st.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	return os.WriteFile(path, b, 0o644)
}

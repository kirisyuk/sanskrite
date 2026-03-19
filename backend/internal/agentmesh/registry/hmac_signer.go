package registry

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

type HMACSigner struct {
	secret string
	keyID  string
}

func NewHMACSigner(secret, keyID string) *HMACSigner {
	return &HMACSigner{
		secret: strings.TrimSpace(secret),
		keyID:  defaultKeyID(keyID),
	}
}

func (s *HMACSigner) Sign(_ context.Context, payload []byte, version string) (*contracts.AgentCardSignature, error) {
	if strings.TrimSpace(s.secret) == "" {
		return nil, fmt.Errorf("agent card signing secret is required")
	}
	mac := hmac.New(sha256.New, []byte(s.secret))
	_, _ = mac.Write(payload)
	return &contracts.AgentCardSignature{
		Alg:       "HS256",
		KeyID:     s.keyID,
		Signature: hex.EncodeToString(mac.Sum(nil)),
		SignedAt:  time.Now().UTC(),
		Version:   strings.TrimSpace(version),
	}, nil
}

func (s *HMACSigner) Verify(_ context.Context, payload []byte, signature *contracts.AgentCardSignature) error {
	if signature == nil {
		return fmt.Errorf("agent card signature is required")
	}
	if strings.TrimSpace(s.secret) == "" {
		return fmt.Errorf("agent card validation secret is required")
	}
	mac := hmac.New(sha256.New, []byte(s.secret))
	_, _ = mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.ToLower(expected)), []byte(strings.ToLower(strings.TrimSpace(signature.Signature)))) {
		return fmt.Errorf("agent card signature mismatch")
	}
	return nil
}

func defaultKeyID(in string) string {
	if strings.TrimSpace(in) != "" {
		return strings.TrimSpace(in)
	}
	return "agentmesh-hmac-v1"
}

package registry

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	kms "cloud.google.com/go/kms/apiv1"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	kmspb "google.golang.org/genproto/googleapis/cloud/kms/v1"
)

type KMSSigner struct {
	client     *kms.KeyManagementClient
	keyVersion string
	keyID      string
	publicKey  crypto.PublicKey
}

type ecdsaSignature struct {
	R, S *big.Int
}

func NewKMSSigner(ctx context.Context, keyVersion, keyID string) (*KMSSigner, error) {
	keyVersion = strings.TrimSpace(keyVersion)
	if keyVersion == "" {
		return nil, fmt.Errorf("kms key version is required")
	}
	client, err := kms.NewKeyManagementClient(ctx)
	if err != nil {
		return nil, err
	}
	pub, err := client.GetPublicKey(ctx, &kmspb.GetPublicKeyRequest{Name: keyVersion})
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	block, _ := pem.Decode([]byte(pub.Pem))
	if block == nil {
		_ = client.Close()
		return nil, fmt.Errorf("kms public key PEM is invalid")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return &KMSSigner{
		client:     client,
		keyVersion: keyVersion,
		keyID:      defaultKMSKeyID(keyID, keyVersion),
		publicKey:  parsed,
	}, nil
}

func (s *KMSSigner) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *KMSSigner) Sign(ctx context.Context, payload []byte, version string) (*contracts.AgentCardSignature, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("kms signer is not configured")
	}
	sum := sha256.Sum256(payload)
	resp, err := s.client.AsymmetricSign(ctx, &kmspb.AsymmetricSignRequest{
		Name: s.keyVersion,
		Digest: &kmspb.Digest{
			Digest: &kmspb.Digest_Sha256{Sha256: sum[:]},
		},
	})
	if err != nil {
		return nil, err
	}
	return &contracts.AgentCardSignature{
		Alg:       "ES256",
		KeyID:     s.keyID,
		Signature: base64.StdEncoding.EncodeToString(resp.Signature),
		SignedAt:  time.Now().UTC(),
		Version:   strings.TrimSpace(version),
	}, nil
}

func (s *KMSSigner) Verify(_ context.Context, payload []byte, signature *contracts.AgentCardSignature) error {
	if s == nil {
		return fmt.Errorf("kms signer is not configured")
	}
	if signature == nil {
		return fmt.Errorf("agent card signature is required")
	}
	pub, ok := s.publicKey.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("kms public key is not ECDSA")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature.Signature))
	if err != nil {
		return err
	}
	var parsed ecdsaSignature
	if _, err := asn1.Unmarshal(raw, &parsed); err != nil {
		return err
	}
	sum := sha256.Sum256(payload)
	if !ecdsa.Verify(pub, sum[:], parsed.R, parsed.S) {
		return fmt.Errorf("agent card signature mismatch")
	}
	return nil
}

func defaultKMSKeyID(in, keyVersion string) string {
	if strings.TrimSpace(in) != "" {
		return strings.TrimSpace(in)
	}
	return strings.TrimSpace(keyVersion)
}

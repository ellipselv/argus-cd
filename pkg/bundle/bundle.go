// Package bundle defines the wire format that argus-occulus signs and
// argus-nexus verifies before deploying.
package bundle

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// Manifest is the signed payload describing a single deployment.
type Manifest struct {
	Project    string `json:"project"`
	Version    string `json:"version"`
	HealthPort int    `json:"health_port"`
	Compose    string `json:"compose"`
}

// Bundle is what Occulus serves: the raw manifest bytes (preserved verbatim so
// the signature stays verifiable) plus a detached base64 Ed25519 signature.
type Bundle struct {
	Manifest  json.RawMessage `json:"manifest"`
	Signature string          `json:"signature"`
}

func (b *Bundle) Verify(pub ed25519.PublicKey) (*Manifest, error) {
	sig, err := base64.StdEncoding.DecodeString(b.Signature)
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(pub, b.Manifest, sig) {
		return nil, errors.New("signature verification failed")
	}
	var m Manifest
	if err := json.Unmarshal(b.Manifest, &m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if m.Project == "" {
		return nil, errors.New("manifest: project is empty")
	}
	if m.Version == "" {
		return nil, errors.New("manifest: version is empty")
	}
	if m.HealthPort <= 0 || m.HealthPort > 65535 {
		return nil, fmt.Errorf("manifest: invalid health_port %d", m.HealthPort)
	}
	if m.Compose == "" {
		return nil, errors.New("manifest: compose is empty")
	}
	return &m, nil
}

func LoadPublicKey(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key size %d, expected %d", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

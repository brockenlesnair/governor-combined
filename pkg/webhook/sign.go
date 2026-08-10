package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
)

// Signer creates HMAC signatures for webhook payloads.
type Signer struct {
	secret []byte
	algo   string
}

// NewSigner creates a signer with the given secret and algorithm.
func NewSigner(secret, algo string) *Signer {
	if algo == "" {
		algo = "sha256"
	}
	return &Signer{
		secret: []byte(secret),
		algo:   algo,
	}
}

// Sign computes HMAC signature for the given payload.
func (s *Signer) Sign(payload []byte) string {
	var h func() hash.Hash
	switch s.algo {
	case "sha512":
		h = sha512.New
	default:
		h = sha256.New
	}

	mac := hmac.New(h, s.secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify checks if a signature matches the payload.
func (s *Signer) Verify(payload []byte, signature string) bool {
	expected := s.Sign(payload)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// HeaderName returns the header name for the signature.
func (s *Signer) HeaderName() string {
	return fmt.Sprintf("X-Webhook-Signature-%s", uppercaseAlgo(s.algo))
}

func uppercaseAlgo(algo string) string {
	switch algo {
	case "sha256":
		return "SHA256"
	case "sha512":
		return "SHA512"
	default:
		return algo
	}
}

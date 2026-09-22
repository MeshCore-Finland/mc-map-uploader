package uploader

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"filippo.io/edwards25519"
)

// CreateKeyFile creates a long-lived signing seed. It refuses to overwrite
// an existing identity; the caller must back up the resulting file.
func CreateKeyFile(path string) error {
	if path == "" {
		return errors.New("key path is required")
	}
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(hex.EncodeToString(seed) + "\n"); err != nil {
		return err
	}
	return file.Sync()
}

func ReadKeyFile(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("map.key_file is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("map key file must be regular and inaccessible to group and others")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	secret, err := hex.DecodeString(strings.TrimSpace(string(content)))
	if err != nil || (len(secret) != 32 && len(secret) != 64) {
		return nil, fmt.Errorf("map key file must contain a 32-byte seed or 64-byte MeshCore expanded key as hex")
	}
	if len(secret) == 64 && !isClamped(secret[:32]) {
		return nil, errors.New("MeshCore expanded key scalar is not clamped")
	}
	return secret, nil
}

func isClamped(scalar []byte) bool {
	return len(scalar) == 32 && scalar[0]&7 == 0 && scalar[31]&0xc0 == 0x40
}

func publicKey(secret []byte) ([]byte, error) {
	if len(secret) == ed25519.SeedSize {
		return ed25519.NewKeyFromSeed(secret).Public().(ed25519.PublicKey), nil
	}
	if len(secret) != 64 || !isClamped(secret[:32]) {
		return nil, errors.New("map signing key must be a 32-byte seed or 64-byte MeshCore expanded key")
	}
	scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(secret[:32])
	if err != nil {
		return nil, err
	}
	return new(edwards25519.Point).ScalarBaseMult(scalar).Bytes(), nil
}

func PublicKeyHex(secret []byte) (string, error) {
	public, err := publicKey(secret)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(public), nil
}

func sign(secret, message []byte) ([]byte, error) {
	if len(secret) == ed25519.SeedSize {
		return ed25519.Sign(ed25519.NewKeyFromSeed(secret), message), nil
	}
	public, err := publicKey(secret)
	if err != nil {
		return nil, err
	}
	scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(secret[:32])
	if err != nil {
		return nil, err
	}
	nonceInput := make([]byte, 0, 32+len(message))
	nonceInput = append(nonceInput, secret[32:]...)
	nonceInput = append(nonceInput, message...)
	nonceHash := sha512.Sum512(nonceInput)
	nonce, err := new(edwards25519.Scalar).SetUniformBytes(nonceHash[:])
	if err != nil {
		return nil, err
	}
	r := new(edwards25519.Point).ScalarBaseMult(nonce).Bytes()
	challengeInput := make([]byte, 0, 64+len(message))
	challengeInput = append(challengeInput, r...)
	challengeInput = append(challengeInput, public...)
	challengeInput = append(challengeInput, message...)
	challengeHash := sha512.Sum512(challengeInput)
	challenge, err := new(edwards25519.Scalar).SetUniformBytes(challengeHash[:])
	if err != nil {
		return nil, err
	}
	s := new(edwards25519.Scalar).MultiplyAdd(challenge, scalar, nonce)
	return append(r, s.Bytes()...), nil
}

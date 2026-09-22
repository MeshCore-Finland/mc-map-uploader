package uploader

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestMeshCoreExpandedKey(t *testing.T) {
	seed := bytes.Repeat([]byte{0x42}, 32)
	digest := sha512.Sum512(seed)
	expanded := append([]byte(nil), digest[:]...)
	expanded[0] &= 248
	expanded[31] &= 63
	expanded[31] |= 64
	public, err := PublicKeyHex(expanded)
	if err != nil || public != hex.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)) {
		t.Fatalf("wrong expanded public key: %s %v", public, err)
	}
	message := []byte("map request digest")
	signature, err := sign(expanded, message)
	if err != nil || !ed25519.Verify(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), message, signature) {
		t.Fatalf("invalid expanded signature: %v", err)
	}
}

func TestPersistentKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map.key")
	if err := CreateKeyFile(path); err != nil {
		t.Fatal(err)
	}
	first, err := ReadKeyFile(path)
	if err != nil || len(first) != 32 {
		t.Fatalf("read key: %v", err)
	}
	if err := CreateKeyFile(path); err == nil {
		t.Fatal("keygen overwrote an existing identity")
	}
	second, err := ReadKeyFile(path)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("identity changed across reads")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadKeyFile(path); err == nil {
		t.Fatal("world-readable key accepted")
	}
}

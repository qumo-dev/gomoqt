package main

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeCertHash(t *testing.T) {
	// create a temporary PEM file containing a fake certificate payload
	payload := []byte{0x01, 0x02, 0x03, 0x04}
	b64 := base64.StdEncoding.EncodeToString(payload)
	pem := "-----BEGIN CERTIFICATE-----\n" + b64 + "\n-----END CERTIFICATE-----\n"

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cert.pem")
	if err := os.WriteFile(path, []byte(pem), 0600); err != nil {
		t.Fatalf("failed to write temp PEM: %v", err)
	}

	hash, err := computeCertHash(path)
	if err != nil {
		t.Fatalf("computeCertHash returned error: %v", err)
	}

	expected := sha256.Sum256(payload)
	expB64 := base64.StdEncoding.EncodeToString(expected[:])
	if hash != expB64 {
		t.Fatalf("hash mismatch: got %s, want %s", hash, expB64)
	}
}

package main

import (
	"context"
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

func TestBuildTSClientCmd_missingMoqWeb(t *testing.T) {
	// Create a temporary root with a go.mod file but no moq-web directory.
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example"), 0600); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	// Change working directory for findRootDir to locate tmpDir
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to tmpDir: %v", err)
	}

	_, err := buildTSClientCmd(context.Background(), "localhost:1234")
	if err == nil {
		t.Fatal("expected error when moq-web is missing")
	}
}

func TestBuildTSClientCmd_success(t *testing.T) {
	// Setup a fake project root with moq-web present
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example"), 0600); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tmpDir, "moq-web"), 0755); err != nil {
		t.Fatalf("failed to create moq-web dir: %v", err)
	}
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to tmpDir: %v", err)
	}

	cmd, err := buildTSClientCmd(context.Background(), "localhost:1234")
	if err != nil {
		t.Fatalf("unexpected error building client cmd: %v", err)
	}
	if cmd.Dir != filepath.Join(tmpDir, "moq-web") {
		t.Fatalf("cmd.Dir = %s; want %s", cmd.Dir, filepath.Join(tmpDir, "moq-web"))
	}
}

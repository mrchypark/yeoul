package main

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGCSEmulatorServiceAccountUsesOnlyLocalTokenEndpoint(t *testing.T) {
	account, err := gcsEmulatorServiceAccount("http://127.0.0.1:4588")
	if err != nil {
		t.Fatal(err)
	}
	var credentials struct {
		Type       string `json:"type"`
		PrivateKey string `json:"private_key"`
		TokenURI   string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(account), &credentials); err != nil {
		t.Fatal("generated service account is not valid JSON")
	}
	if credentials.Type != "service_account" || credentials.TokenURI != "http://127.0.0.1:4588/token" {
		t.Fatal("generated service account has unexpected type or token endpoint")
	}
	block, _ := pem.Decode([]byte(credentials.PrivateKey))
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Fatal("generated service account has no RSA private key")
	}
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
		t.Fatal("generated service account contains an invalid RSA key")
	}
	if strings.Contains(credentials.TokenURI, "googleapis.com") || strings.Contains(credentials.TokenURI, "metadata.google.internal") {
		t.Fatal("generated service account refers to a live token endpoint")
	}
}

func TestGCSEmulatorServiceAccountRejectsNonlocalTokenEndpoint(t *testing.T) {
	if _, err := gcsEmulatorServiceAccount("http://example.com:4588"); err == nil {
		t.Fatal("accepted nonlocal token endpoint")
	}
}

func TestValidateFreshTargetRequiresOwnedAbsentPathAndPreservesNeighbors(t *testing.T) {
	parent := t.TempDir()
	neighbor := filepath.Join(parent, "neighbor")
	if err := os.Mkdir(neighbor, 0o700); err != nil {
		t.Fatal(err)
	}
	neighborFile := filepath.Join(neighbor, "keep")
	if err := os.WriteFile(neighborFile, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(parent, "owned")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	token := "owner-token"
	marker := filepath.Join(root, ".yeoul-legacy-fixture-owner")
	if err := os.WriteFile(marker, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateFreshTarget(filepath.Join(root, "fresh-db"), root, token); err != nil {
		t.Fatalf("valid fresh target rejected: %v", err)
	}

	existing := filepath.Join(root, "existing-db")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(existing, "keep")
	if err := os.WriteFile(sentinel, []byte("preserve existing database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateFreshTarget(existing, root, token); err == nil {
		t.Fatal("accepted an existing database path")
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "preserve existing database" {
		t.Fatalf("existing database was changed: value=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(neighborFile); err != nil || string(got) != "untouched" {
		t.Fatalf("neighbor path was changed: value=%q err=%v", got, err)
	}

	unmarked := filepath.Join(parent, "unmarked")
	if err := os.Mkdir(unmarked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateFreshTarget(filepath.Join(unmarked, "fresh-db"), unmarked, token); err == nil {
		t.Fatal("accepted an unmarked fixture root")
	}
}

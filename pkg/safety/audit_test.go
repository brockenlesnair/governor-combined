package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewAuditLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	al, err := NewAuditLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("audit log file should exist after creation")
	}
}

func TestAuditLog_Log(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	al, err := NewAuditLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	req := &ValidationRequest{Policy: "default", Action: "check"}
	result := &ValidationResult{Passed: true, RiskScore: 1.0}

	if err := al.Log(req, result); err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	info, _ := os.Stat(path)
	if info.Size() == 0 {
		t.Error("audit log should have data after Log")
	}
}

func TestAuditLog_ChainHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	al, err := NewAuditLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	req := &ValidationRequest{Policy: "default"}
	result := &ValidationResult{Passed: true}

	al.Log(req, result)
	firstHash := al.prevHash

	al.Log(req, result)
	secondHash := al.prevHash

	if firstHash == "" {
		t.Error("first entry should have a hash")
	}
	if secondHash == "" {
		t.Error("second entry should have a hash")
	}
	if firstHash == secondHash {
		t.Error("hashes should be different for different entries")
	}
}

func TestAuditLog_VerifyChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	al, err := NewAuditLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	req := &ValidationRequest{Policy: "default"}
	result := &ValidationResult{Passed: true}

	al.Log(req, result)
	al.Log(req, result)
	al.Log(req, result)

	valid, msg := al.VerifyChain()
	if !valid {
		t.Errorf("chain should be valid, got: %s", msg)
	}
}

func TestAuditLog_GetEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	al, err := NewAuditLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	entries, err := al.GetEntries(AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from stub, got %d", len(entries))
	}
}

func TestAuditLog_Close(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	al, err := NewAuditLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := al.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

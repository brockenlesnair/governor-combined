package rigour

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func testDLPFilter() *DLPFilter {
	return NewDLPFilter(slog.Default())
}

func TestNewDLPFilter(t *testing.T) {
	f := testDLPFilter()
	if f == nil {
		t.Fatal("NewDLPFilter returned nil")
	}
	if f.entropyThresh != 4.5 {
		t.Errorf("expected entropyThresh 4.5, got %f", f.entropyThresh)
	}
	if len(f.secretPatterns) == 0 {
		t.Error("expected secret patterns to be populated")
	}
	if len(f.piiPatterns) == 0 {
		t.Error("expected PII patterns to be populated")
	}
}

func TestDLPFilter_Check_Clean(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `package main

import "fmt"

func main() {
	fmt.Println("hello world")
}
`
	result := f.Check(ctx, text, "main.go")
	if result.Blocked {
		t.Error("clean code should not be blocked")
	}
	if len(result.SecretHits) > 0 {
		t.Error("clean code should have no secret hits")
	}
	if len(result.PIIHits) > 0 {
		t.Error("clean code should have no PII hits")
	}
}

func TestDLPFilter_Check_APIKey(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `api_key = "sk-1234567890abcdef12345678"`
	result := f.Check(ctx, text, "config.go")
	if !result.Blocked {
		t.Error("API key should be blocked")
	}
	if len(result.SecretHits) == 0 {
		t.Error("expected at least 1 secret hit")
	}
}

func TestDLPFilter_Check_Token(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `token = "ghp_abcdefghijklmnopqrstuvwxyz1234567890"`
	result := f.Check(ctx, text, "auth.go")
	if !result.Blocked {
		t.Error("token should be blocked")
	}
	if len(result.SecretHits) == 0 {
		t.Error("expected at least 1 secret hit for token")
	}
}

func TestDLPFilter_Check_Password(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `password = "supersecretpassword123"`
	result := f.Check(ctx, text, "config.go")
	if !result.Blocked {
		t.Error("password should be blocked")
	}
}

func TestDLPFilter_Check_BearerToken(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0`
	result := f.Check(ctx, text, "header.go")
	if !result.Blocked {
		t.Error("bearer token should be blocked")
	}
}

func TestDLPFilter_Check_PrivateKey(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGcY5unA67hqlYMd4Prn7dOt
-----END RSA PRIVATE KEY-----`
	result := f.Check(ctx, text, "key.pem")
	if !result.Blocked {
		t.Error("private key should be blocked")
	}
}

func TestDLPFilter_Check_EmailPII(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `Contact the user at john.doe@example.com for more info.`
	result := f.Check(ctx, text, "contacts.go")
	if !result.Blocked {
		t.Error("email PII should be blocked")
	}
	if len(result.PIIHits) == 0 {
		t.Error("expected at least 1 PII hit")
	}
	foundEmail := false
	for _, hit := range result.PIIHits {
		if hit.Type == "email" {
			foundEmail = true
		}
	}
	if !foundEmail {
		t.Error("expected email PII hit")
	}
}

func TestDLPFilter_Check_PhonePII(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `Call us at (555) 123-4567 for support.`
	result := f.Check(ctx, text, "contacts.go")
	if !result.Blocked {
		t.Error("phone PII should be blocked")
	}
	foundPhone := false
	for _, hit := range result.PIIHits {
		if hit.Type == "phone" {
			foundPhone = true
		}
	}
	if !foundPhone {
		t.Error("expected phone PII hit")
	}
}

func TestDLPFilter_Check_SSNPII(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `SSN: 123-45-6789`
	result := f.Check(ctx, text, "pii.go")
	if !result.Blocked {
		t.Error("SSN PII should be blocked")
	}
	foundSSN := false
	for _, hit := range result.PIIHits {
		if hit.Type == "ssn" {
			foundSSN = true
		}
	}
	if !foundSSN {
		t.Error("expected SSN PII hit")
	}
}

func TestDLPFilter_Check_Entropy(t *testing.T) {
	f := testDLPFilter()
	f.SetEntropyThreshold(3.0) // Lower threshold to make test reliable
	ctx := context.Background()

	// High entropy string: random-looking chars spanning the 20-char window
	highEntropy := "aB3dE5gH7jK9mN2pQ4sT6"
	text := "const key = \"" + highEntropy + "\""
	result := f.Check(ctx, text, "secrets.go")

	// Should have entropy hits
	if len(result.EntropyHits) == 0 {
		t.Error("expected entropy hits for high-entropy string")
	}
}

func TestDLPFilter_Check_EntropyNoBlock(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	// Medium entropy string (above 4.5 but below 5.5)
	text := "some random looking text aB3dE5gH7jK9mN2pQ4"
	result := f.Check(ctx, text, "data.txt")

	// Medium entropy alone should not block (only warnings)
	if result.Blocked && len(result.SecretHits) == 0 {
		t.Error("medium entropy without secrets should warn, not block")
	}
}

func TestDLPFilter_Check_Exemptions(t *testing.T) {
	f := testDLPFilter()
	f.AddFalsePositive("test_fixtures")
	f.AddFalsePositive("*_test.go")
	ctx := context.Background()

	text := `api_key = "sk-1234567890abcdef12345678"`
	result := f.Check(ctx, text, "test_fixtures/config.json")
	if result.Blocked {
		t.Error("exempted path should not be blocked")
	}
	if result.FalsePositiveExemptions == 0 {
		t.Error("expected exemption to be counted")
	}
}

func TestDLPFilter_Check_ExemptGlobSuffix(t *testing.T) {
	f := testDLPFilter()
	f.AddFalsePositive("*_test.go")
	ctx := context.Background()

	text := `token = "ghp_abcdefghijklmnopqrstuvwxyz1234567890"`
	result := f.Check(ctx, text, "auth_test.go")
	if result.Blocked {
		t.Error("glob-exempted path should not be blocked")
	}
}

func TestDLPFilter_Check_ExemptGlobPrefix(t *testing.T) {
	f := testDLPFilter()
	f.AddFalsePositive("mock_*")
	ctx := context.Background()

	text := `secret = "supersecretpassword12345"`
	result := f.Check(ctx, text, "mock_auth.go")
	if result.Blocked {
		t.Error("prefix-glob-exempted path should not be blocked")
	}
}

func TestDLPFilter_Check_EmptyText(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	result := f.Check(ctx, "", "main.go")
	if result.Blocked {
		t.Error("empty text should not be blocked")
	}
}

func TestDLPFilter_SetEntropyThreshold(t *testing.T) {
	f := testDLPFilter()
	f.SetEntropyThreshold(2.0)
	if f.entropyThresh != 2.0 {
		t.Errorf("expected entropyThresh 2.0, got %f", f.entropyThresh)
	}
}

func TestShannonEntropy(t *testing.T) {
	tests := []struct {
		input string
		min   float64
		max   float64
	}{
		{"", 0, 0},
		{"aaaaaaaaaa", 0, 0.1},
		{"abcdefghij", 2.0, 4.0},
	}

	for _, tt := range tests {
		e := shannonEntropy(tt.input)
		if e < tt.min || e > tt.max {
			t.Errorf("shannonEntropy(%q) = %f, want [%f, %f]", tt.input, e, tt.min, tt.max)
		}
	}
}

func TestRedact(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short", "***"},
		{"abcdefgh", "abc**fgh"},
		{"abcdef", "***"},
		{"abcdefghij", "abc****hij"},
	}

	for _, tt := range tests {
		got := redact(tt.input)
		if got != tt.want {
			t.Errorf("redact(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchGlobSimple(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*", "anything", true},
		{"prefix*", "prefix_something", true},
		{"prefix*", "something_else", false},
		{"*suffix", "something_suffix", true},
		{"*suffix", "something_else", false},
		{"exact", "exact", true},
		{"exact", "not_exact", false},
	}

	for _, tt := range tests {
		got := matchGlobSimple(tt.pattern, tt.name)
		if got != tt.want {
			t.Errorf("matchGlobSimple(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

func TestDLPFilter_Check_CloudSecret(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`
	result := f.Check(ctx, text, "config.go")
	if !result.Blocked {
		t.Error("AWS secret key should be blocked")
	}
}

func TestDLPFilter_Check_ClientSecret(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `client_secret = "abcdefghijklmnopqrstuvwxyz1234567890"`
	result := f.Check(ctx, text, "oauth.go")
	if !result.Blocked {
		t.Error("client_secret should be blocked")
	}
}

func TestDLPFilter_Check_CreditCard(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	// Visa test number format
	text := `card = "4111111111111111"`
	result := f.Check(ctx, text, "payment.go")
	if !result.Blocked {
		t.Error("credit card number should be blocked")
	}
	if len(result.PIIHits) == 0 {
		t.Error("expected credit card PII hit")
	}
}

func TestDLPFilter_Reasons(t *testing.T) {
	f := testDLPFilter()
	ctx := context.Background()

	text := `api_key = "sk-1234567890abcdef12345678"`
	result := f.Check(ctx, text, "config.go")

	if len(result.Reasons) == 0 {
		t.Error("expected at least 1 reason for blocked content")
	}

	foundSecretReason := false
	for _, r := range result.Reasons {
		if strings.Contains(r, "secret") {
			foundSecretReason = true
		}
	}
	if !foundSecretReason {
		t.Error("expected a reason mentioning 'secret'")
	}
}

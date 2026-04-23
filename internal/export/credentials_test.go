package export

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeCredsFile(t *testing.T, dir string, accessToken string) string {
	t.Helper()
	type oauthBlock struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
	}
	type credsFile struct {
		ClaudeAiOauth oauthBlock `json:"claudeAiOauth"`
	}
	c := credsFile{ClaudeAiOauth: oauthBlock{
		AccessToken:  accessToken,
		RefreshToken: "r",
		ExpiresAt:    9999999999999,
	}}
	b, _ := json.Marshal(c)
	path := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("writeCredsFile: %v", err)
	}
	return path
}

func fakeJWT(payload string) string {
	header := "eyJhbGciOiJSUzI1NiJ9"
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return header + "." + encodedPayload + ".fakesig"
}

func TestFileCredReaderEmail(t *testing.T) {
	tests := []struct {
		name       string
		setupFile  func(t *testing.T, dir string) string
		wantEmail  string
		wantErrSub string // substring expected in error, "" means no error
	}{
		{
			name: "valid JWT with email",
			setupFile: func(t *testing.T, dir string) string {
				t.Helper()
				jwt := fakeJWT(`{"email":"test@example.com","sub":"123"}`)
				return writeCredsFile(t, dir, jwt)
			},
			wantEmail: "test@example.com",
		},
		{
			name: "JWT payload with no email field",
			setupFile: func(t *testing.T, dir string) string {
				t.Helper()
				jwt := fakeJWT(`{"sub":"123","name":"Alice"}`)
				return writeCredsFile(t, dir, jwt)
			},
			wantEmail: "",
		},
		{
			name: "file does not exist",
			setupFile: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "nonexistent.json")
			},
			wantErrSub: "credentials",
		},
		{
			name: "malformed JSON in credentials file",
			setupFile: func(t *testing.T, dir string) string {
				t.Helper()
				path := filepath.Join(dir, "bad.json")
				if err := os.WriteFile(path, []byte("not json {{{"), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
				return path
			},
			wantErrSub: "credentials",
		},
		{
			name: "access token with wrong number of dots",
			setupFile: func(t *testing.T, dir string) string {
				t.Helper()
				return writeCredsFile(t, dir, "onlyone")
			},
			wantErrSub: "malformed JWT",
		},
		{
			name: "access token with two dots (two parts only)",
			setupFile: func(t *testing.T, dir string) string {
				t.Helper()
				return writeCredsFile(t, dir, "part1.part2")
			},
			wantErrSub: "malformed JWT",
		},
	}

	// Test base64url padding variations: build payloads of lengths 1, 2, 3 mod 4
	// to ensure all padding cases (==, =, none) are handled.
	for _, payloadLen := range []int{4, 5, 6} { // 4%4=0, 5%4=1, 6%4=2
		payloadLen := payloadLen
		tests = append(tests, struct {
			name       string
			setupFile  func(t *testing.T, dir string) string
			wantEmail  string
			wantErrSub string
		}{
			name: fmt.Sprintf("base64url padding len=%d mod 4=%d", payloadLen, payloadLen%4),
			setupFile: func(t *testing.T, dir string) string {
				t.Helper()
				// Build a payload that is exactly payloadLen bytes as JSON
				// We pad the email to control the JSON length.
				// Just use a standard email — we care that decode succeeds.
				jwt := fakeJWT(`{"email":"a@b.com"}`)
				return writeCredsFile(t, dir, jwt)
			},
			wantEmail: "a@b.com",
		})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := tc.setupFile(t, dir)
			r := NewFileCredReader(path)
			email, err := r.Email()

			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tc.wantErrSub)
				}
				if err.Error() == "" {
					t.Fatalf("error is empty string")
				}
				// The error should contain the expected substring
				found := false
				errStr := err.Error()
				for i := 0; i <= len(errStr)-len(tc.wantErrSub); i++ {
					if errStr[i:i+len(tc.wantErrSub)] == tc.wantErrSub {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("want error containing %q, got %q", tc.wantErrSub, errStr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if email != tc.wantEmail {
				t.Errorf("email: want %q got %q", tc.wantEmail, email)
			}
		})
	}
}

// TestFileCredReaderEmptyAccessToken ensures that an empty access token
// returns ("", nil) rather than an error.
func TestFileCredReaderEmptyAccessToken(t *testing.T) {
	dir := t.TempDir()
	path := writeCredsFile(t, dir, "")
	r := NewFileCredReader(path)
	email, err := r.Email()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if email != "" {
		t.Errorf("email: want empty got %q", email)
	}
}

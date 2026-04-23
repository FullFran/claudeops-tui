package export

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// CredReader abstracts reading the user's email from stored credentials.
type CredReader interface {
	Email() (string, error)
}

// NewFileCredReader returns a CredReader that reads the user email from the
// JWT access token stored in ~/.claude/.credentials.json.
// Returns "" when credentials are absent or the token contains no email claim.
func NewFileCredReader(path string) CredReader {
	return &fileCredReader{path: path}
}

type fileCredReader struct{ path string }

func (r *fileCredReader) Email() (string, error) {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return "", fmt.Errorf("credentials: read %s: %w", r.path, err)
	}
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("credentials: parse: %w", err)
	}
	token := creds.ClaudeAiOauth.AccessToken
	if token == "" {
		return "", nil
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("credentials: malformed JWT (got %d parts)", len(parts))
	}
	// Decode base64url payload, trying RawURLEncoding first (no padding),
	// then URLEncoding with added padding for robustness.
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Try adding padding and using standard URLEncoding.
		padded := parts[1]
		switch len(padded) % 4 {
		case 2:
			padded += "=="
		case 3:
			padded += "="
		}
		raw, err = base64.URLEncoding.DecodeString(padded)
		if err != nil {
			return "", fmt.Errorf("credentials: decode JWT payload: %w", err)
		}
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", fmt.Errorf("credentials: parse JWT claims: %w", err)
	}
	return claims.Email, nil
}

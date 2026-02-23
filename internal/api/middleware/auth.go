package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────
// Auth context keys
// ──────────────────────────────────────────────────

type contextKey string

const (
	ContextKeyUser contextKey = "user"
)

// UserInfo represents the authenticated user extracted from JWT.
type UserInfo struct {
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Groups   []string `json:"groups"`
	Issuer   string   `json:"iss"`
	Subject  string   `json:"sub"`
	TokenID  string   `json:"jti"`
	IssuedAt int64    `json:"iat"`
	Expiry   int64    `json:"exp"`
}

// IsInternal returns true if this is an internal service token.
func (u *UserInfo) IsInternal() bool {
	return u.Issuer == "TerrakubeInternal"
}

// IsPAT returns true if this is a Personal Access Token.
func (u *UserInfo) IsPAT() bool {
	return u.Issuer == "Terrakube"
}

// IsServiceAccount returns true for PAT or internal tokens.
func (u *UserInfo) IsServiceAccount() bool {
	return u.IsPAT() || u.IsInternal()
}

// IsMember checks if the user belongs to a given group.
func (u *UserInfo) IsMember(group string) bool {
	for _, g := range u.Groups {
		if g == group {
			return true
		}
	}
	return false
}

// GetUser extracts UserInfo from the request context.
func GetUser(ctx context.Context) *UserInfo {
	user, _ := ctx.Value(ContextKeyUser).(*UserInfo)
	return user
}

// ──────────────────────────────────────────────────
// Auth middleware configuration
// ──────────────────────────────────────────────────

// AuthConfig holds auth middleware configuration.
type AuthConfig struct {
	// Dex OIDC issuer URI (e.g. https://dex.example.com)
	DexIssuerURI string
	// HMAC secret for PAT tokens (base64url-encoded)
	PatSecret string
	// HMAC secret for internal tokens (base64url-encoded)
	InternalSecret string
	// Instance owner group (superuser)
	OwnerGroup string
	// UI URL for CORS
	UIURL string
}

// ──────────────────────────────────────────────────
// Public path matching
// ──────────────────────────────────────────────────

var publicPaths = []string{
	"/.well-known/terraform.json",
	"/.well-known/openid-configuration",
	"/.well-known/jwks",
	"/actuator/",
	"/callback/v1/",
	"/webhook/v1/",
	"/remote/tfe/v2/ping",
	"/health",
}

var publicPrefixPaths = []string{
	"/remote/tfe/v2/plans/logs/",
	"/remote/tfe/v2/applies/logs/",
	"/tofu/index.json",
}

func isPublicPath(path string, method string) bool {
	// OPTIONS are always public
	if method == http.MethodOptions {
		return true
	}

	for _, p := range publicPaths {
		if path == p || strings.HasPrefix(path, p) {
			return true
		}
	}

	for _, p := range publicPrefixPaths {
		if strings.HasPrefix(path, p) {
			return true
		}
	}

	// PUT to specific paths
	if method == http.MethodPut {
		if strings.HasPrefix(path, "/remote/tfe/v2/configuration-versions/") ||
			strings.HasPrefix(path, "/tfstate/v1/archive/") {
			return true
		}
	}

	return false
}

// ──────────────────────────────────────────────────
// Auth Middleware
// ──────────────────────────────────────────────────

// AuthMiddleware validates JWT tokens and sets UserInfo in context.
func AuthMiddleware(config AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for public paths
			if isPublicPath(r.URL.Path, r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			// Extract Bearer token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"errors":[{"status":"401","title":"Unauthorized","detail":"Missing Authorization header"}]}`, http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				http.Error(w, `{"errors":[{"status":"401","title":"Unauthorized","detail":"Invalid Authorization header format"}]}`, http.StatusUnauthorized)
				return
			}

			// Decode JWT claims (without signature verification first to determine issuer)
			claims, err := decodeJWTClaims(token)
			if err != nil {
				log.Printf("Failed to decode JWT claims: %v", err)
				http.Error(w, `{"errors":[{"status":"401","title":"Unauthorized","detail":"Invalid token"}]}`, http.StatusUnauthorized)
				return
			}

			// Verify token based on issuer type
			switch claims.Issuer {
			case "Terrakube":
				// PAT token — verify with HMAC secret
				if err := verifyHMACToken(token, config.PatSecret); err != nil {
					log.Printf("PAT token verification failed: %v", err)
					http.Error(w, `{"errors":[{"status":"401","title":"Unauthorized","detail":"Invalid PAT token"}]}`, http.StatusUnauthorized)
					return
				}
			case "TerrakubeInternal":
				// Internal service token — verify with internal secret
				if err := verifyHMACToken(token, config.InternalSecret); err != nil {
					log.Printf("Internal token verification failed: %v", err)
					http.Error(w, `{"errors":[{"status":"401","title":"Unauthorized","detail":"Invalid internal token"}]}`, http.StatusUnauthorized)
					return
				}
			default:
				// Dex/OIDC token — verify with OIDC provider
				// For now, we trust the JWT format and validate expiry.
				// Full OIDC verification (JWK validation) will be added when
				// we integrate with the OIDC discovery endpoint.
				if claims.Expiry > 0 && time.Now().Unix() > claims.Expiry {
					http.Error(w, `{"errors":[{"status":"401","title":"Unauthorized","detail":"Token expired"}]}`, http.StatusUnauthorized)
					return
				}
			}

			// Check token expiry
			if claims.Expiry > 0 && time.Now().Unix() > claims.Expiry {
				http.Error(w, `{"errors":[{"status":"401","title":"Unauthorized","detail":"Token expired"}]}`, http.StatusUnauthorized)
				return
			}

			// Set user in context
			ctx := context.WithValue(r.Context(), ContextKeyUser, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ──────────────────────────────────────────────────
// CORS Middleware
// ──────────────────────────────────────────────────

// CORSMiddleware adds CORS headers.
func CORSMiddleware(uiURL string) func(http.Handler) http.Handler {
	origins := strings.Split(uiURL, ",")
	log.Printf("CORS: configured allowed origins: %v (raw UIURL=%q)", origins, uiURL)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := false
			for _, o := range origins {
				if strings.TrimSpace(o) == origin {
					allowed = true
					break
				}
			}

			if r.Method == http.MethodOptions {
				log.Printf("CORS: OPTIONS request origin=%q allowed=%v", origin, allowed)
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Access-Control-Allow-Headers,Access-Control-Allow-Origin,Access-Control-Request-Method,Access-Control-Request-Headers,Origin,Cache-Control,Content-Type,Accept,Authorization,X-TFC-Token,X-TFC-Url")
				w.Header().Set("Access-Control-Allow-Methods", "DELETE,GET,POST,PATCH,PUT,OPTIONS")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ──────────────────────────────────────────────────
// JWT helpers
// ──────────────────────────────────────────────────

// decodeJWTClaims decodes the JWT payload without verifying the signature.
func decodeJWTClaims(token string) (*UserInfo, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	// Decode payload (part 1)
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims UserInfo
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	// Parse groups from different claim formats
	var raw map[string]interface{}
	json.Unmarshal(payload, &raw)
	if groups, ok := raw["groups"]; ok {
		switch g := groups.(type) {
		case []interface{}:
			for _, v := range g {
				if s, ok := v.(string); ok {
					claims.Groups = append(claims.Groups, s)
				}
			}
		}
	}

	return &claims, nil
}

// verifyHMACToken verifies a JWT signed with HMAC-SHA256.
func verifyHMACToken(token string, secretBase64URL string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid JWT format")
	}

	// Decode the secret
	secret, err := base64URLDecode(secretBase64URL)
	if err != nil {
		return fmt.Errorf("invalid secret: %w", err)
	}

	// Compute HMAC-SHA256 of header.payload
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expectedSig := mac.Sum(nil)

	// Decode the provided signature
	actualSig, err := base64URLDecode(parts[2])
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

func base64URLDecode(s string) ([]byte, error) {
	// Add padding if necessary
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}

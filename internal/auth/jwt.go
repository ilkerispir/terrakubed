package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GenerateTerrakubeToken mimics the token generation from Terrakube Executor Java
func GenerateTerrakubeToken(internalSecret string) (string, error) {
	if internalSecret == "" {
		return "", fmt.Errorf("InternalSecret is not configured, cannot generate Terrakube Token")
	}

	decodedSecret, err := decodeSecret(internalSecret)
	if err != nil {
		return "", err
	}

	claims := jwt.MapClaims{
		"iss":            "TerrakubeInternal",
		"sub":            "TerrakubeInternal (TOKEN)",
		"aud":            "TerrakubeInternal",
		"email":          "no-reply@terrakube.io",
		"email_verified": true,
		"name":           "TerrakubeInternal Client",
		"iat":            time.Now().Unix(),
		"exp":            time.Now().Add(30 * 24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signedToken, err := token.SignedString(decodedSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign Terrakube JWT: %w", err)
	}

	return signedToken, nil
}

// ValidateToken validates a JWT token using the internal secret, PAT secret,
// or OIDC issuer URI (for Dex/OAuth tokens from the UI).
// Returns the claims if valid.
func ValidateToken(tokenString, internalSecret, patSecret string) (jwt.MapClaims, error) {
	return ValidateTokenWithIssuer(tokenString, internalSecret, patSecret, "")
}

// ValidateTokenWithIssuer validates a JWT token. For tokens issued by Dex/OIDC,
// it fetches the JWKS from the issuerUri's discovery endpoint to validate the signature.
func ValidateTokenWithIssuer(tokenString, internalSecret, patSecret, issuerUri string) (jwt.MapClaims, error) {
	// Parse without validation first to get the issuer
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	unverified, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := unverified.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}

	issuer, _ := claims["iss"].(string)

	switch issuer {
	case "TerrakubeInternal":
		return validateHMACToken(tokenString, internalSecret, "internal secret")
	case "Terrakube":
		return validateHMACToken(tokenString, patSecret, "PAT secret")
	default:
		// Dex/OIDC token — validate using JWKS from the issuer
		if issuerUri == "" {
			return nil, fmt.Errorf("unsupported token issuer: %s (no issuer URI configured)", issuer)
		}
		return validateOIDCToken(tokenString, issuerUri)
	}
}

func validateHMACToken(tokenString, secretStr, secretName string) (jwt.MapClaims, error) {
	if secretStr == "" {
		return nil, fmt.Errorf("%s not configured", secretName)
	}
	secret, err := decodeSecret(secretStr)
	if err != nil {
		return nil, err
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is not valid")
	}

	return token.Claims.(jwt.MapClaims), nil
}

// JWKS cache
var (
	jwksCache    map[string]*jwksData
	jwksCacheMu  sync.RWMutex
	jwksCacheTTL = 10 * time.Minute
)

func init() {
	jwksCache = make(map[string]*jwksData)
}

type jwksData struct {
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
}

type oidcDiscovery struct {
	JwksUri string `json:"jwks_uri"`
}

func getJWKS(issuerUri string) (map[string]*rsa.PublicKey, error) {
	jwksCacheMu.RLock()
	cached, ok := jwksCache[issuerUri]
	jwksCacheMu.RUnlock()

	if ok && time.Since(cached.fetchedAt) < jwksCacheTTL {
		return cached.keys, nil
	}

	// Discover JWKS URI from OIDC discovery endpoint
	discoveryURL := fmt.Sprintf("%s/.well-known/openid-configuration", issuerUri)
	resp, err := http.Get(discoveryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC discovery: %w", err)
	}
	defer resp.Body.Close()

	var discovery oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("failed to decode OIDC discovery: %w", err)
	}

	if discovery.JwksUri == "" {
		return nil, fmt.Errorf("no jwks_uri in OIDC discovery response")
	}

	// Fetch the JWKS
	jwksResp, err := http.Get(discovery.JwksUri)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer jwksResp.Body.Close()

	var jwks jwksResponse
	if err := json.NewDecoder(jwksResp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey)
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" {
			continue
		}
		pubKey, err := parseRSAPublicKey(key)
		if err != nil {
			log.Printf("Warning: failed to parse JWKS key %s: %v", key.Kid, err)
			continue
		}
		keys[key.Kid] = pubKey
	}

	jwksCacheMu.Lock()
	jwksCache[issuerUri] = &jwksData{
		keys:      keys,
		fetchedAt: time.Now(),
	}
	jwksCacheMu.Unlock()

	return keys, nil
}

func parseRSAPublicKey(key jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

func validateOIDCToken(tokenString, issuerUri string) (jwt.MapClaims, error) {
	keys, err := getJWKS(issuerUri)
	if err != nil {
		return nil, fmt.Errorf("failed to get JWKS: %w", err)
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		kid, ok := token.Header["kid"].(string)
		if !ok {
			// If no kid, try the first key
			for _, key := range keys {
				return key, nil
			}
			return nil, fmt.Errorf("no keys available for OIDC validation")
		}

		key, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("no matching key found for kid: %s", kid)
		}
		return key, nil
	})
	if err != nil {
		return nil, fmt.Errorf("OIDC token validation failed: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("OIDC token is not valid")
	}

	return token.Claims.(jwt.MapClaims), nil
}

func decodeSecret(secret string) ([]byte, error) {
	decoded, err := base64.URLEncoding.DecodeString(secret)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(secret)
		if err != nil {
			return nil, fmt.Errorf("failed to decode secret: %w", err)
		}
	}
	return decoded, nil
}

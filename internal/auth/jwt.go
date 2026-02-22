package auth

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GenerateTerrakubeToken mimics the token generation from Terrakube Executor Java
func GenerateTerrakubeToken(internalSecret string) (string, error) {
	if internalSecret == "" {
		return "", fmt.Errorf("InternalSecret is not configured, cannot generate Terrakube Token")
	}

	// The Java executor decodes the Base64URL string into raw bytes
	decodedSecret, err := base64.URLEncoding.DecodeString(internalSecret)
	if err != nil {
		// Fallback to standard base64 if URL encoding fails
		decodedSecret, err = base64.StdEncoding.DecodeString(internalSecret)
		if err != nil {
			return "", fmt.Errorf("failed to decode InternalSecret: %w", err)
		}
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
	// Typ header is automatically added as "JWT" by golang-jwt

	signedToken, err := token.SignedString(decodedSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign Terrakube JWT: %w", err)
	}

	return signedToken, nil
}

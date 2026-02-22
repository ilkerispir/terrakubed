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

// ValidateToken validates a JWT token using either the internal secret or PAT secret.
// Returns the claims if valid.
func ValidateToken(tokenString, internalSecret, patSecret string) (jwt.MapClaims, error) {
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

	var secret []byte
	switch issuer {
	case "TerrakubeInternal":
		if internalSecret == "" {
			return nil, fmt.Errorf("internal secret not configured")
		}
		secret, err = decodeSecret(internalSecret)
		if err != nil {
			return nil, err
		}
	case "Terrakube":
		if patSecret == "" {
			return nil, fmt.Errorf("PAT secret not configured")
		}
		secret, err = decodeSecret(patSecret)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported token issuer: %s", issuer)
	}

	// Now verify with the correct secret
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

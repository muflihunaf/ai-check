package auth

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const userIDKey contextKey = "authUserID"

// GetUserID retrieves the authenticated subject from context.
func GetUserID(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	if value, ok := ctx.Value(userIDKey).(string); ok && value != "" {
		return value, true
	}
	return "", false
}

// JWTMiddleware validates bearer tokens and injects user identity.
func JWTMiddleware(secret, audience string) gin.HandlerFunc {
	secret = strings.TrimSpace(secret)
	audience = strings.TrimSpace(audience)

	return func(c *gin.Context) {
		tokenString, err := extractBearerToken(c.Request.Header.Get("Authorization"))
		if err != nil {
			unauthorized(c, err.Error())
			return
		}

		if secret == "" {
			secret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
		}
		if secret == "" {
			unauthorized(c, "missing JWT secret")
			return
		}

		claims := &jwt.RegisteredClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			unauthorized(c, "invalid token")
			return
		}

		if audience == "" {
			audience = strings.TrimSpace(os.Getenv("JWT_AUDIENCE"))
		}
		if audience != "" && !containsAudience(claims.Audience, audience) {
			unauthorized(c, "invalid audience")
			return
		}

		if claims.Subject == "" {
			unauthorized(c, "missing subject")
			return
		}

		ctx := context.WithValue(c.Request.Context(), userIDKey, claims.Subject)
		c.Request = c.Request.WithContext(ctx)
		c.Set(string(userIDKey), claims.Subject)

		c.Next()
	}
}

func extractBearerToken(header string) (string, error) {
	if header == "" {
		return "", errors.New("authorization header required")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errors.New("invalid authorization header")
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", errors.New("token missing")
	}
	return token, nil
}

func unauthorized(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": message})
}

func containsAudience(claims jwt.ClaimStrings, expected string) bool {
	for _, aud := range claims {
		if aud == expected {
			return true
		}
	}
	return false
}

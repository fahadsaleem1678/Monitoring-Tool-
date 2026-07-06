package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const userContextKey contextKey = "auth_user"

type User struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
}

type Claims struct {
	Sub      string `json:"sub"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

type Service struct {
	secret []byte
}

func NewService(secret string) *Service {
	return &Service{secret: []byte(secret)}
}

func (s *Service) Sign(user User, ttl time.Duration) (string, error) {
	claims := Claims{
		Sub:      user.ID.String(),
		Username: user.Username,
		Role:     user.Role,
		Exp:      time.Now().Add(ttl).Unix(),
	}

	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString(headerJSON)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	unsigned := encodedHeader + "." + encodedClaims
	return unsigned + "." + s.signature(unsigned), nil
}

func (s *Service) Verify(token string) (User, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return User{}, errors.New("invalid token")
	}
	unsigned := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(s.signature(unsigned))) {
		return User{}, errors.New("invalid token signature")
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return User{}, err
	}
	var claims Claims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return User{}, err
	}
	if time.Now().Unix() > claims.Exp {
		return User{}, errors.New("token expired")
	}

	id, err := uuid.Parse(claims.Sub)
	if err != nil {
		return User{}, err
	}
	return User{ID: id, Username: claims.Username, Role: claims.Role}, nil
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			writeUnauthorized(w, "missing bearer token")
			return
		}

		user, err := s.Verify(strings.TrimSpace(token))
		if err != nil {
			writeUnauthorized(w, err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok || user.Role != "admin" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"error":"admin role required"}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey).(User)
	return user, ok
}

func (s *Service) signature(unsigned string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"error":%q}`, message)
}

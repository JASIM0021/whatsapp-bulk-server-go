package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

// Auth returns a middleware that validates JWT tokens.
// It checks the Authorization header first, then falls back to ?token= query param
// (needed for SSE EventSource which cannot set headers).
func Auth(authService *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := ""

			// 1. Authorization: Bearer <token>
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				tokenStr = strings.TrimPrefix(auth, "Bearer ")
			}
			// 2. ?token=<token> (SSE fallback)
			if tokenStr == "" {
				tokenStr = r.URL.Query().Get("token")
			}

			if tokenStr == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"success":false,"error":"unauthorized"}`))
				return
			}

			userID, err := authService.ValidateToken(tokenStr)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"success":false,"error":"invalid token"}`))
				return
			}

			ctx := context.WithValue(r.Context(), types.UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserID retrieves the authenticated user's ID from the request context.
func GetUserID(r *http.Request) (string, bool) {
	userID, ok := r.Context().Value(types.UserIDKey).(string)
	return userID, ok
}

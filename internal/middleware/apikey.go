package middleware

import (
	"context"
	"net/http"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

// APIKeyAuth validates the X-API-Key header, checks active subscription, and injects userID.
func APIKeyAuth(apiKeySvc *service.APIKeyService, subSvc *service.SubscriptionService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"success":false,"error":"missing API key"}`))
				return
			}

			userID, err := apiKeySvc.ValidateAPIKey(r.Context(), key)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"success":false,"error":"invalid or inactive API key"}`))
				return
			}

			active, err := subSvc.IsSubscriptionActive(r.Context(), userID)
			if err != nil || !active {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"success":false,"error":"subscription_expired"}`))
				return
			}

			// Update last_used_at in background — non-blocking
			go apiKeySvc.UpdateLastUsed(context.Background(), key)

			ctx := context.WithValue(r.Context(), types.UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

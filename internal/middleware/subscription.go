package middleware

import (
	"net/http"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
)

// RequireSubscription returns middleware that checks if the authenticated user
// has an active subscription before allowing access.
func RequireSubscription(subService *service.SubscriptionService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := GetUserID(r)
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"success":false,"error":"unauthorized"}`))
				return
			}

			active, err := subService.IsSubscriptionActive(r.Context(), userID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"success":false,"error":"failed to check subscription"}`))
				return
			}

			if !active {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"success":false,"error":"subscription_expired","message":"Your subscription has expired. Please upgrade to continue."}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

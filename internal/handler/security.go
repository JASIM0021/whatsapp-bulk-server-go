package handler

import (
	"encoding/json"
	"net/http"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/middleware"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

// SecurityHandler exposes security settings and the activity heartbeat endpoint.
type SecurityHandler struct {
	securityService *service.SecurityService
}

func NewSecurityHandler(securityService *service.SecurityService) *SecurityHandler {
	return &SecurityHandler{securityService: securityService}
}

// HandleSettings serves GET and POST for /api/settings/security.
func (h *SecurityHandler) HandleSettings(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := h.securityService.GetSettings(r.Context(), userID)
		if err != nil {
			respondError(w, "Failed to get security settings", http.StatusInternalServerError)
			return
		}
		respondJSON(w, types.APIResponse{Success: true, Data: settings})

	case http.MethodPost:
		var req struct {
			AutoLogoutEnabled bool `json:"autoLogoutEnabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := h.securityService.UpdateAutoLogout(r.Context(), userID, req.AutoLogoutEnabled); err != nil {
			respondError(w, "Failed to update security settings", http.StatusInternalServerError)
			return
		}
		respondJSON(w, types.APIResponse{Success: true, Message: "Security settings updated"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// Heartbeat handles POST /api/activity/heartbeat.
// The frontend calls this periodically when the user is active and auto-logout is enabled.
func (h *SecurityHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.securityService.RecordActivity(r.Context(), userID); err != nil {
		respondError(w, "Failed to record activity", http.StatusInternalServerError)
		return
	}

	respondJSON(w, types.APIResponse{Success: true})
}

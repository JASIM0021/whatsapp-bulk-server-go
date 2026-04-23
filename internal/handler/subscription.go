package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/logger"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/middleware"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

type SubscriptionHandler struct {
	subService  *service.SubscriptionService
	authService *service.AuthService
}

func NewSubscriptionHandler(subService *service.SubscriptionService, authService *service.AuthService) *SubscriptionHandler {
	return &SubscriptionHandler{subService: subService, authService: authService}
}

// GetSubscription returns the current user's subscription info.
func (h *SubscriptionHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	info, err := h.subService.GetSubscription(r.Context(), userID)
	if err != nil {
		respondError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, types.APIResponse{Success: true, Data: info})
}

// InitiatePayment creates a payment and returns PayU form data.
func (h *SubscriptionHandler) InitiatePayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req types.InitiatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	validPlans := map[string]bool{
		types.PlanMonthly: true, types.PlanYearly: true,
		types.PlanStarter: true, types.PlanStarterYearly: true,
		types.PlanGrowth: true, types.PlanGrowthYearly: true,
		types.PlanBusiness: true, types.PlanBusinessYearly: true,
		types.PlanAddonMessages: true,
	}
	if !validPlans[req.Plan] {
		respondError(w, "Invalid plan", http.StatusBadRequest)
		return
	}

	// Get user info for PayU form
	user, err := h.authService.GetUser(r.Context(), userID)
	if err != nil {
		respondError(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	formData, err := h.subService.InitiatePayment(r.Context(), userID, req.Plan, user.Email, user.Name, "", req.PromoCode)
	if err != nil {
		respondError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, types.APIResponse{Success: true, Data: formData})
}

// PaymentSuccess handles PayU's POST callback on successful payment.
// This endpoint is PUBLIC (no JWT) — security is via PayU hash verification.
func (h *SubscriptionHandler) PaymentSuccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Error("Failed to parse PayU success form: %v", err)
		http.Redirect(w, r, h.subService.GetFrontendURL()+"/payment/failure?error=parse_error", http.StatusSeeOther)
		return
	}

	params := make(map[string]string)
	for key := range r.PostForm {
		params[key] = r.PostFormValue(key)
	}

	txnID, err := h.subService.HandlePaymentSuccess(r.Context(), params)
	if err != nil {
		logger.Error("Payment success handler error: %v", err)
		http.Redirect(w, r, fmt.Sprintf("%s/payment/failure?error=%s", h.subService.GetFrontendURL(), err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("%s/payment/success?txnid=%s", h.subService.GetFrontendURL(), txnID), http.StatusSeeOther)
}

// PaymentFailure handles PayU's POST callback on failed payment.
// This endpoint is PUBLIC (no JWT).
func (h *SubscriptionHandler) PaymentFailure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Error("Failed to parse PayU failure form: %v", err)
		http.Redirect(w, r, h.subService.GetFrontendURL()+"/payment/failure?error=parse_error", http.StatusSeeOther)
		return
	}

	params := make(map[string]string)
	for key := range r.PostForm {
		params[key] = r.PostFormValue(key)
	}

	txnID, _ := h.subService.HandlePaymentFailure(r.Context(), params)
	http.Redirect(w, r, fmt.Sprintf("%s/payment/failure?txnid=%s", h.subService.GetFrontendURL(), txnID), http.StatusSeeOther)
}

// GetPublicPlanPricing returns live plan pricing (no auth required).
func (h *SubscriptionHandler) GetPublicPlanPricing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pricing := h.subService.GetPublicPlanPricing(r.Context())
	respondJSON(w, types.APIResponse{Success: true, Data: pricing})
}

// ValidatePromoCode validates a promo code for a given plan.
// GET /api/payment/validate-promo?code=CODE&plan=monthly
func (h *SubscriptionHandler) ValidatePromoCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := r.URL.Query().Get("code")
	plan := r.URL.Query().Get("plan")

	if code == "" {
		respondError(w, "code is required", http.StatusBadRequest)
		return
	}
	if plan != types.PlanMonthly && plan != types.PlanYearly {
		respondError(w, "plan must be 'monthly' or 'yearly'", http.StatusBadRequest)
		return
	}

	result, err := h.subService.ValidatePromoCode(r.Context(), code, plan)
	if err != nil {
		respondError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, types.APIResponse{Success: true, Data: result})
}

// GetPaymentHistory returns the current user's payment history.
func (h *SubscriptionHandler) GetPaymentHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	payments, err := h.subService.GetPaymentHistory(r.Context(), userID)
	if err != nil {
		respondError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, types.APIResponse{Success: true, Data: payments})
}

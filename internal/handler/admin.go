package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/middleware"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

// AdminHandler handles admin panel API endpoints.
type AdminHandler struct {
	db                  *mongo.Database
	authService         *service.AuthService
	emailService        *service.EmailService
	subscriptionService *service.SubscriptionService
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(db *mongo.Database, authService *service.AuthService, emailService *service.EmailService, subscriptionService *service.SubscriptionService) *AdminHandler {
	return &AdminHandler{db: db, authService: authService, emailService: emailService, subscriptionService: subscriptionService}
}

// GetStats returns high-level dashboard statistics.
// GET /api/admin/stats
func (h *AdminHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	totalUsers, err := h.db.Collection("users").CountDocuments(ctx, bson.M{})
	if err != nil {
		respondError(w, "Failed to count users", http.StatusInternalServerError)
		return
	}

	now := time.Now()

	activeSubscriptions, err := h.db.Collection("subscriptions").CountDocuments(ctx, bson.M{
		"status":      "active",
		"expiry_date": bson.M{"$gt": now},
	})
	if err != nil {
		respondError(w, "Failed to count active subscriptions", http.StatusInternalServerError)
		return
	}

	expiredSubscriptions, err := h.db.Collection("subscriptions").CountDocuments(ctx, bson.M{
		"$or": []bson.M{
			{"status": "expired"},
			{"expiry_date": bson.M{"$lte": now}},
		},
	})
	if err != nil {
		respondError(w, "Failed to count expired subscriptions", http.StatusInternalServerError)
		return
	}

	trialUsers, err := h.db.Collection("subscriptions").CountDocuments(ctx, bson.M{
		"plan": "free",
	})
	if err != nil {
		respondError(w, "Failed to count trial users", http.StatusInternalServerError)
		return
	}

	paidUsers, err := h.db.Collection("subscriptions").CountDocuments(ctx, bson.M{
		"plan": bson.M{"$in": []string{"monthly", "yearly"}},
	})
	if err != nil {
		respondError(w, "Failed to count paid users", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"totalUsers":           totalUsers,
			"activeSubscriptions":  activeSubscriptions,
			"expiredSubscriptions": expiredSubscriptions,
			"trialUsers":           trialUsers,
			"paidUsers":            paidUsers,
		},
	})
}

// ListUsers returns a paginated list of users with subscription info.
// GET /api/admin/users?search=email&page=1&limit=20
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	search := r.URL.Query().Get("search")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	skip := int64((page - 1) * limit)

	// Build match filter
	matchFilter := bson.M{}
	if search != "" {
		matchFilter["email"] = bson.M{"$regex": search, "$options": "i"}
	}

	// Count total matching users
	total, err := h.db.Collection("users").CountDocuments(ctx, matchFilter)
	if err != nil {
		respondError(w, "Failed to count users", http.StatusInternalServerError)
		return
	}

	// Aggregation pipeline: users + lookup subscriptions
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchFilter}},
		{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}}}},
		{{Key: "$skip", Value: skip}},
		{{Key: "$limit", Value: int64(limit)}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "subscriptions",
			"localField":   "_id",
			"foreignField": "user_id",
			"as":           "subscriptions",
		}}},
	}

	cursor, err := h.db.Collection("users").Aggregate(ctx, pipeline)
	if err != nil {
		respondError(w, "Failed to list users", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		respondError(w, "Failed to decode users", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	users := make([]map[string]interface{}, 0, len(results))
	for _, doc := range results {
		userStatus, _ := doc["status"].(string)
		if userStatus == "" {
			userStatus = "active"
		}
		user := map[string]interface{}{
			"id":        doc["_id"].(primitive.ObjectID).Hex(),
			"email":     doc["email"],
			"name":      doc["name"],
			"role":      doc["role"],
			"status":    userStatus,
			"createdAt": doc["created_at"],
		}

		// Attach subscription info if available
		subs, _ := doc["subscriptions"].(primitive.A)
		if len(subs) > 0 {
			sub, _ := subs[0].(bson.M)
			if sub != nil {
				expiryDate, _ := sub["expiry_date"].(primitive.DateTime)
				expiry := expiryDate.Time()
				isActive := sub["status"] == "active" && expiry.After(now)
				daysLeft := 0
				if isActive {
					daysLeft = int(math.Ceil(expiry.Sub(now).Hours() / 24))
				}
				status, _ := sub["status"].(string)
				if !isActive && status == "active" {
					status = "expired"
				}
				user["subscription"] = map[string]interface{}{
					"plan":        sub["plan"],
					"status":      status,
					"expiryDate": expiry.Format("2006-01-02"),
					"isActive":   isActive,
					"daysLeft":   daysLeft,
				}
			}
		}

		users = append(users, user)
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"users": users,
			"total": total,
			"page":  page,
			"limit": limit,
		},
	})
}

// CreateUser creates a new user from the admin panel.
// POST /api/admin/users
func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.Email == "" || body.Password == "" || body.Name == "" {
		respondError(w, "Email, password and name are required", http.StatusBadRequest)
		return
	}
	if body.Role == "" {
		body.Role = "user"
	}
	if body.Role != "admin" && body.Role != "user" {
		respondError(w, "Role must be 'admin' or 'user'", http.StatusBadRequest)
		return
	}

	// Register user via auth service (creates user + trial subscription + default templates)
	resp, err := h.authService.Register(r.Context(), types.RegisterRequest{
		Email:    body.Email,
		Password: body.Password,
		Name:     body.Name,
	})
	if err != nil {
		respondError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Set role if admin
	if body.Role == "admin" {
		oid, _ := primitive.ObjectIDFromHex(resp.User.ID)
		h.db.Collection("users").UpdateOne(r.Context(), bson.M{"_id": oid}, bson.M{"$set": bson.M{"role": "admin"}})
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"message": "User created successfully",
		"data": map[string]interface{}{
			"id":    resp.User.ID,
			"email": resp.User.Email,
			"name":  resp.User.Name,
			"role":  body.Role,
		},
	})
}

// GetUser returns a single user with subscription info.
// GET /api/admin/users/{id}
func (h *AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := extractUserIDFromPath(r.URL.Path)
	if userID == "" {
		respondError(w, "User ID is required", http.StatusBadRequest)
		return
	}

	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		respondError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Aggregation to get user + subscription
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"_id": oid}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "subscriptions",
			"localField":   "_id",
			"foreignField": "user_id",
			"as":           "subscriptions",
		}}},
	}

	cursor, err := h.db.Collection("users").Aggregate(ctx, pipeline)
	if err != nil {
		respondError(w, "Failed to find user", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil || len(results) == 0 {
		respondError(w, "User not found", http.StatusNotFound)
		return
	}

	doc := results[0]
	now := time.Now()
	getUserStatus, _ := doc["status"].(string)
	if getUserStatus == "" {
		getUserStatus = "active"
	}
	user := map[string]interface{}{
		"id":        doc["_id"].(primitive.ObjectID).Hex(),
		"email":     doc["email"],
		"name":      doc["name"],
		"role":      doc["role"],
		"status":    getUserStatus,
		"createdAt": doc["created_at"],
	}

	subs, _ := doc["subscriptions"].(primitive.A)
	if len(subs) > 0 {
		sub, _ := subs[0].(bson.M)
		if sub != nil {
			expiryDate, _ := sub["expiry_date"].(primitive.DateTime)
			expiry := expiryDate.Time()
			isActive := sub["status"] == "active" && expiry.After(now)
			daysLeft := 0
			if isActive {
				daysLeft = int(math.Ceil(expiry.Sub(now).Hours() / 24))
			}
			status, _ := sub["status"].(string)
			if !isActive && status == "active" {
				status = "expired"
			}
			user["subscription"] = map[string]interface{}{
				"plan":        sub["plan"],
				"status":      status,
				"expiry_date": expiry.Format("2006-01-02"),
				"is_active":   isActive,
				"days_left":   daysLeft,
			}
		}
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"data":    user,
	})
}

// UpdateUser updates a user's role or name.
// PUT /api/admin/users/{id}
func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := extractUserIDFromPath(r.URL.Path)
	if userID == "" {
		respondError(w, "User ID is required", http.StatusBadRequest)
		return
	}

	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		respondError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var body struct {
		Role   *string `json:"role"`
		Name   *string `json:"name"`
		Status *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	update := bson.M{}
	if body.Role != nil {
		if *body.Role != "admin" && *body.Role != "user" {
			respondError(w, "Role must be 'admin' or 'user'", http.StatusBadRequest)
			return
		}
		update["role"] = *body.Role
	}
	if body.Name != nil {
		update["name"] = *body.Name
	}
	if body.Status != nil {
		if *body.Status != "active" && *body.Status != "blocked" {
			respondError(w, "Status must be 'active' or 'blocked'", http.StatusBadRequest)
			return
		}
		update["status"] = *body.Status
	}

	if len(update) == 0 {
		respondError(w, "No fields to update", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	result, err := h.db.Collection("users").UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	if err != nil {
		respondError(w, "Failed to update user", http.StatusInternalServerError)
		return
	}
	if result.MatchedCount == 0 {
		respondError(w, "User not found", http.StatusNotFound)
		return
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"message": "User updated successfully",
	})
}

// DeleteUser deletes a user and all associated data.
// DELETE /api/admin/users/{id}
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	targetUserID := extractUserIDFromPath(r.URL.Path)
	if targetUserID == "" {
		respondError(w, "User ID is required", http.StatusBadRequest)
		return
	}

	// Don't allow deleting yourself
	currentUserID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if currentUserID == targetUserID {
		respondError(w, "Cannot delete your own account", http.StatusBadRequest)
		return
	}

	oid, err := primitive.ObjectIDFromHex(targetUserID)
	if err != nil {
		respondError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Delete user
	result, err := h.db.Collection("users").DeleteOne(ctx, bson.M{"_id": oid})
	if err != nil {
		respondError(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}
	if result.DeletedCount == 0 {
		respondError(w, "User not found", http.StatusNotFound)
		return
	}

	// Delete associated data (best-effort, don't fail the request)
	h.db.Collection("subscriptions").DeleteMany(ctx, bson.M{"user_id": oid})
	h.db.Collection("templates").DeleteMany(ctx, bson.M{"user_id": oid})
	h.db.Collection("payments").DeleteMany(ctx, bson.M{"user_id": oid})

	respondJSON(w, map[string]interface{}{
		"success": true,
		"message": "User and associated data deleted successfully",
	})
}

// SendPromotionalEmail sends promotional emails to filtered recipients.
// POST /api/admin/email/promotional
func (h *AdminHandler) SendPromotionalEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Subject    string `json:"subject"`
		Body       string `json:"body"`
		Recipients string `json:"recipients"` // "all", "active", "trial"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if body.Subject == "" || body.Body == "" {
		respondError(w, "Subject and body are required", http.StatusBadRequest)
		return
	}
	if body.Recipients == "" {
		body.Recipients = "all"
	}

	ctx := r.Context()
	now := time.Now()

	var userIDs []primitive.ObjectID

	switch body.Recipients {
	case "active":
		// Users with active subscriptions
		cursor, err := h.db.Collection("subscriptions").Find(ctx, bson.M{
			"status":      "active",
			"expiry_date": bson.M{"$gt": now},
		}, options.Find().SetProjection(bson.M{"user_id": 1}))
		if err != nil {
			respondError(w, "Failed to query subscriptions", http.StatusInternalServerError)
			return
		}
		defer cursor.Close(ctx)
		var docs []bson.M
		if err := cursor.All(ctx, &docs); err != nil {
			respondError(w, "Failed to decode subscriptions", http.StatusInternalServerError)
			return
		}
		for _, d := range docs {
			if uid, ok := d["user_id"].(primitive.ObjectID); ok {
				userIDs = append(userIDs, uid)
			}
		}

	case "trial":
		// Users on free/trial plan
		cursor, err := h.db.Collection("subscriptions").Find(ctx, bson.M{
			"plan": "free",
		}, options.Find().SetProjection(bson.M{"user_id": 1}))
		if err != nil {
			respondError(w, "Failed to query subscriptions", http.StatusInternalServerError)
			return
		}
		defer cursor.Close(ctx)
		var docs []bson.M
		if err := cursor.All(ctx, &docs); err != nil {
			respondError(w, "Failed to decode subscriptions", http.StatusInternalServerError)
			return
		}
		for _, d := range docs {
			if uid, ok := d["user_id"].(primitive.ObjectID); ok {
				userIDs = append(userIDs, uid)
			}
		}

	case "all":
		// All users — no filter needed, we'll query users directly below
	default:
		respondError(w, "Recipients must be 'all', 'active', or 'trial'", http.StatusBadRequest)
		return
	}

	// Fetch user emails and names
	userFilter := bson.M{}
	if body.Recipients != "all" {
		if len(userIDs) == 0 {
			respondJSON(w, map[string]interface{}{
				"success": true,
				"data": map[string]interface{}{
					"sent":   0,
					"failed": 0,
					"total":  0,
				},
			})
			return
		}
		userFilter["_id"] = bson.M{"$in": userIDs}
	}

	cursor, err := h.db.Collection("users").Find(ctx, userFilter,
		options.Find().SetProjection(bson.M{"email": 1, "name": 1}))
	if err != nil {
		respondError(w, "Failed to query users", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var userDocs []bson.M
	if err := cursor.All(ctx, &userDocs); err != nil {
		respondError(w, "Failed to decode users", http.StatusInternalServerError)
		return
	}

	recipients := make([]service.EmailRecipient, 0, len(userDocs))
	for _, u := range userDocs {
		email, _ := u["email"].(string)
		name, _ := u["name"].(string)
		if email != "" {
			recipients = append(recipients, service.EmailRecipient{
				Email: email,
				Name:  name,
			})
		}
	}

	if len(recipients) == 0 {
		respondJSON(w, map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"sent":   0,
				"failed": 0,
				"total":  0,
			},
		})
		return
	}

	errs := h.emailService.SendBulkPromotionalEmail(recipients, body.Subject, body.Body)

	sent := 0
	failed := 0
	var errorMessages []string
	for i, e := range errs {
		if e != nil {
			failed++
			errorMessages = append(errorMessages, recipients[i].Email+": "+e.Error())
		} else {
			sent++
		}
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"sent":   sent,
			"failed": failed,
			"total":  len(recipients),
			"errors": errorMessages,
		},
	})
}

// extractUserIDFromPath extracts the user ID segment from a /api/admin/users/{id} path.
func extractUserIDFromPath(path string) string {
	return extractIDFromPath(path, "/api/admin/users/")
}

// extractIDFromPath extracts the last non-empty, slash-free segment after the given prefix.
func extractIDFromPath(path, prefix string) string {
	trimmed := strings.TrimPrefix(path, prefix)
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return ""
	}
	return trimmed
}

// ─── Invoice handlers ─────────────────────────────────────────────────────────

// ListInvoices returns paginated invoices.
// GET /api/admin/invoices?status=&page=&limit=
func (h *AdminHandler) ListInvoices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := r.URL.Query().Get("status")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	invoices, total, err := h.subscriptionService.ListInvoices(r.Context(), status, page, limit)
	if err != nil {
		respondError(w, "Failed to fetch invoices", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"invoices": invoices,
			"total":    total,
			"page":     page,
			"limit":    limit,
		},
	})
}

// UpdateInvoice updates the final amount of a pending invoice.
// PUT /api/admin/invoices/{id}
func (h *AdminHandler) UpdateInvoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	invoiceID := extractIDFromPath(r.URL.Path, "/api/admin/invoices/")
	if invoiceID == "" {
		respondError(w, "Invoice ID is required", http.StatusBadRequest)
		return
	}

	var req types.UpdateInvoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.subscriptionService.UpdateInvoiceAmount(r.Context(), invoiceID, req.FinalAmount); err != nil {
		respondError(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, map[string]interface{}{"success": true, "message": "Invoice updated"})
}

// ApproveInvoice approves and sends an invoice email to the user.
// POST /api/admin/invoices/{id}/approve
func (h *AdminHandler) ApproveInvoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Path: /api/admin/invoices/{id}/approve
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/admin/invoices/")
	trimmed = strings.TrimSuffix(trimmed, "/approve")
	parts := strings.SplitN(trimmed, "/", 2)
	invoiceID := parts[0]

	if invoiceID == "" {
		respondError(w, "Invoice ID is required", http.StatusBadRequest)
		return
	}

	var req types.ApproveInvoiceRequest
	json.NewDecoder(r.Body).Decode(&req) // body is optional

	if err := h.subscriptionService.ApproveAndSendInvoice(r.Context(), invoiceID, req.Amount); err != nil {
		respondError(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, map[string]interface{}{"success": true, "message": "Invoice approved and sent"})
}

// ─── Plan config handlers ──────────────────────────────────────────────────────

// GetPlanConfigs returns current pricing config for all plans.
// GET /api/admin/plans
func (h *AdminHandler) GetPlanConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	planNames := []string{
		"starter", "starter_yearly",
		"growth", "growth_yearly",
		"business", "business_yearly",
		"addon_messages",
		"free", "monthly", "yearly",
	}
	result := make([]types.PlanConfig, 0, len(planNames))
	for _, plan := range planNames {
		amount, messageLimit, err := h.subscriptionService.GetPlanConfig(ctx, plan)
		if err != nil {
			continue
		}
		result = append(result, types.PlanConfig{
			Plan:         plan,
			Amount:       amount,
			MessageLimit: messageLimit,
		})
	}

	respondJSON(w, map[string]interface{}{"success": true, "data": result})
}

// UpdatePlanConfig updates pricing/limits for a plan.
// PUT /api/admin/plans/{plan}
func (h *AdminHandler) UpdatePlanConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	planName := extractIDFromPath(r.URL.Path, "/api/admin/plans/")
	if planName == "" {
		respondError(w, "Plan name is required", http.StatusBadRequest)
		return
	}
	validPlans := map[string]bool{
		"free": true, "monthly": true, "yearly": true,
		"starter": true, "starter_yearly": true,
		"growth": true, "growth_yearly": true,
		"business": true, "business_yearly": true,
		"addon_messages": true,
	}
	if !validPlans[planName] {
		respondError(w, "Invalid plan name", http.StatusBadRequest)
		return
	}

	var req types.UpdatePlanConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.subscriptionService.UpdatePlanConfig(r.Context(), planName, req.Amount, req.MessageLimit); err != nil {
		respondError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]interface{}{"success": true, "message": "Plan config updated"})
}

// ─── Promo Code handlers ───────────────────────────────────────────────────────

// ListPromoCodes returns all promo codes.
// GET /api/admin/promos
func (h *AdminHandler) ListPromoCodes(w http.ResponseWriter, r *http.Request) {
	promos, err := h.subscriptionService.ListPromoCodes(r.Context())
	if err != nil {
		respondError(w, "Failed to fetch promo codes", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]interface{}{"success": true, "data": promos})
}

// CreatePromoCode creates a new promo code.
// POST /api/admin/promos
func (h *AdminHandler) CreatePromoCode(w http.ResponseWriter, r *http.Request) {
	var req types.CreatePromoCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Code == "" {
		respondError(w, "code is required", http.StatusBadRequest)
		return
	}
	if req.DiscountType != "percentage" && req.DiscountType != "fixed" {
		respondError(w, "discountType must be 'percentage' or 'fixed'", http.StatusBadRequest)
		return
	}
	if req.DiscountValue <= 0 {
		respondError(w, "discountValue must be greater than 0", http.StatusBadRequest)
		return
	}
	if req.DiscountType == "percentage" && req.DiscountValue > 100 {
		respondError(w, "percentage discountValue must be between 0 and 100", http.StatusBadRequest)
		return
	}

	adminID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	promo, err := h.subscriptionService.CreatePromoCode(r.Context(), req, adminID)
	if err != nil {
		respondError(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, map[string]interface{}{"success": true, "data": promo})
}

// UpdatePromoCode partially updates a promo code.
// PUT /api/admin/promos/{id}
func (h *AdminHandler) UpdatePromoCode(w http.ResponseWriter, r *http.Request) {
	id := extractIDFromPath(r.URL.Path, "/api/admin/promos/")
	if id == "" {
		respondError(w, "Promo code ID is required", http.StatusBadRequest)
		return
	}

	var req types.UpdatePromoCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.subscriptionService.UpdatePromoCode(r.Context(), id, req); err != nil {
		respondError(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, map[string]interface{}{"success": true, "message": "Promo code updated"})
}

// DeletePromoCode deletes a promo code by ID.
// DELETE /api/admin/promos/{id}
func (h *AdminHandler) DeletePromoCode(w http.ResponseWriter, r *http.Request) {
	id := extractIDFromPath(r.URL.Path, "/api/admin/promos/")
	if id == "" {
		respondError(w, "Promo code ID is required", http.StatusBadRequest)
		return
	}

	if err := h.subscriptionService.DeletePromoCode(r.Context(), id); err != nil {
		respondError(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, map[string]interface{}{"success": true, "message": "Promo code deleted"})
}

// ─── User activity handler ─────────────────────────────────────────────────────

// GetUserActivity returns payment history and subscription stats for a user.
// GET /api/admin/users/{id}/activity
func (h *AdminHandler) GetUserActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse: /api/admin/users/{id}/activity
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/admin/users/")
	parts := strings.SplitN(trimmed, "/", 2)
	userID := parts[0]

	if userID == "" {
		respondError(w, "User ID is required", http.StatusBadRequest)
		return
	}

	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		respondError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Fetch payments
	cursor, err := h.db.Collection("payments").Find(ctx,
		bson.M{"user_id": oid},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}),
	)
	if err != nil {
		respondError(w, "Failed to fetch payments", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var payDocs []bson.M
	if err := cursor.All(ctx, &payDocs); err != nil {
		respondError(w, "Failed to decode payments", http.StatusInternalServerError)
		return
	}

	payments := make([]map[string]interface{}, 0, len(payDocs))
	for _, d := range payDocs {
		var idStr, userIDStr string
		if id, ok := d["_id"].(primitive.ObjectID); ok {
			idStr = id.Hex()
		}
		if uid, ok := d["user_id"].(primitive.ObjectID); ok {
			userIDStr = uid.Hex()
		}
		payments = append(payments, map[string]interface{}{
			"id":        idStr,
			"userId":    userIDStr,
			"txnId":     d["txn_id"],
			"amount":    d["amount"],
			"plan":      d["plan"],
			"status":    d["status"],
			"mihpayId":  d["mihpay_id"],
			"createdAt": d["created_at"],
		})
	}

	// Fetch subscription
	var subDoc bson.M
	h.db.Collection("subscriptions").FindOne(ctx, bson.M{"user_id": oid}).Decode(&subDoc) //nolint:errcheck

	subInfo := map[string]interface{}{
		"messagesUsed": 0,
		"messageLimit": 0,
		"plan":         "",
		"status":       "",
	}
	if subDoc != nil {
		subInfo["messagesUsed"] = subDoc["messages_used"]
		subInfo["messageLimit"] = subDoc["message_limit"]
		subInfo["plan"] = subDoc["plan"]
		subInfo["status"] = subDoc["status"]
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"payments":     payments,
			"subscription": subInfo,
		},
	})
}

// UpdateUserPlan manually sets a user's subscription plan.
// PUT /api/admin/users/{id}/plan
func (h *AdminHandler) UpdateUserPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract user ID from path: /api/admin/users/{id}/plan
	path := r.URL.Path
	path = strings.TrimSuffix(path, "/plan")
	userID := extractUserIDFromPath(path)
	if userID == "" {
		respondError(w, "User ID is required", http.StatusBadRequest)
		return
	}

	var req types.UpdateUserPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	validAdminPlans := map[string]int{
		"free":               7,
		"starter":            30,
		"starter_yearly":     365,
		"growth":             30,
		"growth_yearly":      365,
		"business":           30,
		"business_yearly":    365,
		"monthly":            30,
		"yearly":             365,
		"unlimited_monthly":  36500,
		"unlimited_yearly":   36500,
	}
	defaultDays, ok := validAdminPlans[req.Plan]
	if !ok {
		respondError(w, "invalid plan", http.StatusBadRequest)
		return
	}

	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		respondError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	daysToAdd := req.DaysToAdd
	if daysToAdd <= 0 {
		daysToAdd = defaultDays
	}

	now := time.Now()
	expiry := now.Add(time.Duration(daysToAdd) * 24 * time.Hour)

	ctx := r.Context()
	update := bson.M{
		"$set": bson.M{
			"plan":          req.Plan,
			"status":        "active",
			"start_date":    now,
			"expiry_date":   expiry,
			"messages_used": 0,
		},
	}
	result, err := h.db.Collection("subscriptions").UpdateOne(ctx, bson.M{"user_id": oid}, update)
	if err != nil {
		respondError(w, "Failed to update subscription", http.StatusInternalServerError)
		return
	}
	if result.MatchedCount == 0 {
		respondError(w, "User subscription not found", http.StatusNotFound)
		return
	}

	respondJSON(w, map[string]interface{}{
		"success": true,
		"message": "User plan updated successfully",
		"data": map[string]interface{}{
			"plan":       req.Plan,
			"expiryDate": expiry.Format("2006-01-02"),
		},
	})
}

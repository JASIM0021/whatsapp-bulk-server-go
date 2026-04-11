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
	db           *mongo.Database
	authService  *service.AuthService
	emailService *service.EmailService
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(db *mongo.Database, authService *service.AuthService, emailService *service.EmailService) *AdminHandler {
	return &AdminHandler{db: db, authService: authService, emailService: emailService}
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

// extractUserIDFromPath extracts the last path segment as a user ID.
// e.g., "/api/admin/users/6612a..." → "6612a..."
func extractUserIDFromPath(path string) string {
	trimmed := strings.TrimPrefix(path, "/api/admin/users/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		return ""
	}
	return trimmed
}

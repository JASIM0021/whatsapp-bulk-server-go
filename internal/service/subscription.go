package service

import (
	"context"
	"crypto/sha512"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/logger"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

// Plan pricing in INR
var planPricing = map[string]float64{
	"monthly": 500,
	"yearly":  5000,
}

// Plan durations
var planDuration = map[string]time.Duration{
	"free":    7 * 24 * time.Hour,
	"monthly": 30 * 24 * time.Hour,
	"yearly":  365 * 24 * time.Hour,
}

// Free trial message limit
const FreeMessageLimit = 50

type subscriptionDoc struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	UserID       primitive.ObjectID `bson:"user_id"`
	Plan         string             `bson:"plan"`
	Status       string             `bson:"status"`
	StartDate    time.Time          `bson:"start_date"`
	ExpiryDate   time.Time          `bson:"expiry_date"`
	CreatedAt    time.Time          `bson:"created_at"`
	MessagesUsed int                `bson:"messages_used"`
	MessageLimit int                `bson:"message_limit"`
}

type paymentDoc struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	UserID       primitive.ObjectID `bson:"user_id"`
	TxnID        string             `bson:"txn_id"`
	Amount       float64            `bson:"amount"`
	Plan         string             `bson:"plan"`
	Status       string             `bson:"status"`
	PayUResponse string             `bson:"payu_response"`
	MihpayID     string             `bson:"mihpay_id"`
	CreatedAt    time.Time          `bson:"created_at"`
}

type SubscriptionService struct {
	db          *db.DB
	emailSvc    *EmailService
	merchantKey string
	salt        string
	baseURL     string
	backendURL  string
	frontendURL string
}

func NewSubscriptionService(database *db.DB) *SubscriptionService {
	baseURL := os.Getenv("PAYU_BASE_URL")
	if baseURL == "" {
		baseURL = "https://secure.payu.in"
	}
	backendURL := os.Getenv("BACKEND_URL")
	if backendURL == "" {
		backendURL = "http://localhost:4000"
	}
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5174"
	}
	return &SubscriptionService{
		db:          database,
		merchantKey: os.Getenv("PAYU_MERCHANT_KEY"),
		salt:        os.Getenv("PAYU_SALT"),
		baseURL:     baseURL,
		backendURL:  backendURL,
		frontendURL: frontendURL,
	}
}

// SetEmailService sets the email service reference.
func (s *SubscriptionService) SetEmailService(emailSvc *EmailService) {
	s.emailSvc = emailSvc
}

// CreateTrialSubscription creates a free trial with 50 message limit for a new user.
func (s *SubscriptionService) CreateTrialSubscription(ctx context.Context, userID string) error {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	now := time.Now()
	// Free trial: no time expiry, limited by message count (50 messages)
	doc := subscriptionDoc{
		ID:           primitive.NewObjectID(),
		UserID:       oid,
		Plan:         types.PlanFree,
		Status:       "active",
		StartDate:    now,
		ExpiryDate:   now.Add(100 * 365 * 24 * time.Hour), // far future — free plan doesn't expire by time
		CreatedAt:    now,
		MessagesUsed: 0,
		MessageLimit: FreeMessageLimit,
	}

	_, err = s.db.Subscriptions().InsertOne(ctx, doc)
	if err != nil {
		// May already exist (duplicate registration attempt)
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return fmt.Errorf("failed to create trial: %w", err)
	}
	logger.Info("Created free trial for user %s (%d messages)", userID, FreeMessageLimit)
	return nil
}

// EnsureTrialSubscription creates a trial if no subscription exists.
// For existing free plans, ensures message_limit field is set.
func (s *SubscriptionService) EnsureTrialSubscription(ctx context.Context, userID string) error {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	var doc subscriptionDoc
	err = s.db.Subscriptions().FindOne(ctx, bson.M{"user_id": oid}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return s.CreateTrialSubscription(ctx, userID)
	}
	if err != nil {
		return fmt.Errorf("database error: %w", err)
	}

	// Migrate old free plans: ensure message_limit is set
	if doc.Plan == types.PlanFree && doc.MessageLimit == 0 {
		_, err = s.db.Subscriptions().UpdateOne(ctx,
			bson.M{"_id": doc.ID},
			bson.M{"$set": bson.M{
				"status":        "active",
				"message_limit": FreeMessageLimit,
				"expiry_date":   time.Now().Add(100 * 365 * 24 * time.Hour),
			}},
		)
		if err != nil {
			return fmt.Errorf("failed to migrate trial: %w", err)
		}
		logger.Info("Migrated free trial for user %s to message-based (%d limit)", userID, FreeMessageLimit)
	}
	return nil
}

// GetSubscription returns the subscription info for a user.
func (s *SubscriptionService) GetSubscription(ctx context.Context, userID string) (*types.SubscriptionInfo, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}

	var doc subscriptionDoc
	err = s.db.Subscriptions().FindOne(ctx, bson.M{"user_id": oid}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		// No subscription — return expired free plan
		return &types.SubscriptionInfo{
			Plan:         types.PlanFree,
			Status:       "expired",
			IsActive:     false,
			DaysLeft:     0,
			MessagesUsed: 0,
			MessageLimit: FreeMessageLimit,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	now := time.Now()
	isActive := doc.Status == "active"

	if doc.Plan == types.PlanFree {
		// Free plan: active if messages not exhausted
		limit := doc.MessageLimit
		if limit == 0 {
			limit = FreeMessageLimit
		}
		if doc.MessagesUsed >= limit {
			isActive = false
			if doc.Status == "active" {
				s.db.Subscriptions().UpdateOne(ctx, bson.M{"_id": doc.ID}, bson.M{"$set": bson.M{"status": "expired"}})
			}
		}
	} else {
		// Paid plans: active if time not expired
		isActive = doc.Status == "active" && doc.ExpiryDate.After(now)
		if doc.Status == "active" && !doc.ExpiryDate.After(now) {
			s.db.Subscriptions().UpdateOne(ctx, bson.M{"_id": doc.ID}, bson.M{"$set": bson.M{"status": "expired"}})
			isActive = false
		}
	}

	daysLeft := 0
	if isActive && doc.Plan != types.PlanFree {
		daysLeft = int(math.Ceil(doc.ExpiryDate.Sub(now).Hours() / 24))
	}

	status := doc.Status
	if !isActive && status == "active" {
		status = "expired"
	}

	messageLimit := doc.MessageLimit
	if doc.Plan == types.PlanFree && messageLimit == 0 {
		messageLimit = FreeMessageLimit
	}

	return &types.SubscriptionInfo{
		Plan:         doc.Plan,
		Status:       status,
		ExpiryDate:   doc.ExpiryDate.Format("2006-01-02"),
		IsActive:     isActive,
		DaysLeft:     daysLeft,
		MessagesUsed: doc.MessagesUsed,
		MessageLimit: messageLimit,
	}, nil
}

// IsSubscriptionActive checks if a user has an active subscription.
func (s *SubscriptionService) IsSubscriptionActive(ctx context.Context, userID string) (bool, error) {
	info, err := s.GetSubscription(ctx, userID)
	if err != nil {
		return false, err
	}
	return info.IsActive, nil
}

// IncrementMessageCount increments the message count for a free-plan user.
// Returns (messagesUsed, messageLimit, error). For paid plans this is a no-op.
func (s *SubscriptionService) IncrementMessageCount(ctx context.Context, userID string, count int) (int, int, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid user ID")
	}

	var doc subscriptionDoc
	err = s.db.Subscriptions().FindOne(ctx, bson.M{"user_id": oid}).Decode(&doc)
	if err != nil {
		return 0, 0, fmt.Errorf("subscription not found")
	}

	// Only track for free plan
	if doc.Plan != types.PlanFree {
		return 0, 0, nil // unlimited for paid plans
	}

	limit := doc.MessageLimit
	if limit == 0 {
		limit = FreeMessageLimit
	}

	newCount := doc.MessagesUsed + count
	update := bson.M{"$set": bson.M{"messages_used": newCount}}
	if newCount >= limit {
		update["$set"].(bson.M)["status"] = "expired"
	}

	_, err = s.db.Subscriptions().UpdateOne(ctx, bson.M{"_id": doc.ID}, update)
	if err != nil {
		return doc.MessagesUsed, limit, fmt.Errorf("failed to update message count: %w", err)
	}

	return newCount, limit, nil
}

// CheckMessageQuota checks if a free-plan user has enough messages remaining.
// Returns (remaining, error). For paid plans returns -1 (unlimited).
func (s *SubscriptionService) CheckMessageQuota(ctx context.Context, userID string) (int, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return 0, fmt.Errorf("invalid user ID")
	}

	var doc subscriptionDoc
	err = s.db.Subscriptions().FindOne(ctx, bson.M{"user_id": oid}).Decode(&doc)
	if err != nil {
		return 0, fmt.Errorf("subscription not found")
	}

	if doc.Plan != types.PlanFree {
		return -1, nil // unlimited
	}

	limit := doc.MessageLimit
	if limit == 0 {
		limit = FreeMessageLimit
	}

	remaining := limit - doc.MessagesUsed
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}

// InitiatePayment creates a payment record and returns PayU form data.
func (s *SubscriptionService) InitiatePayment(ctx context.Context, userID, plan, email, name, phone string) (*types.PayUFormData, error) {
	amount, ok := planPricing[plan]
	if !ok {
		return nil, fmt.Errorf("invalid plan: %s (must be 'monthly' or 'yearly')", plan)
	}

	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}

	txnID := fmt.Sprintf("TXN_%s_%d", userID[:8], time.Now().UnixNano())
	amountStr := fmt.Sprintf("%.2f", amount)
	productInfo := fmt.Sprintf("BulkSend %s Plan", strings.Title(plan))

	// Store pending payment
	payDoc := paymentDoc{
		ID:        primitive.NewObjectID(),
		UserID:    oid,
		TxnID:     txnID,
		Amount:    amount,
		Plan:      plan,
		Status:    "pending",
		CreatedAt: time.Now(),
	}
	if _, err := s.db.Payments().InsertOne(ctx, payDoc); err != nil {
		return nil, fmt.Errorf("failed to create payment record: %w", err)
	}

	// Generate PayU hash
	udf1 := userID
	udf2 := plan
	hash := generatePayUHash(s.merchantKey, txnID, amountStr, productInfo, name, email, udf1, udf2, "", "", "", s.salt)

	logger.Info("Payment initiated: txn=%s user=%s plan=%s amount=%s", txnID, userID, plan, amountStr)

	return &types.PayUFormData{
		Action:      s.baseURL + "/_payment",
		Key:         s.merchantKey,
		TxnID:       txnID,
		Amount:      amountStr,
		ProductInfo: productInfo,
		FirstName:   name,
		Email:       email,
		Phone:       phone,
		Surl:        s.backendURL + "/api/payment/success",
		Furl:        s.backendURL + "/api/payment/failure",
		Hash:        hash,
		Udf1:        udf1,
		Udf2:        udf2,
		Udf3:        "",
		Udf4:        "",
		Udf5:        "",
	}, nil
}

// HandlePaymentSuccess processes a successful PayU callback.
func (s *SubscriptionService) HandlePaymentSuccess(ctx context.Context, params map[string]string) (string, error) {
	// Verify hash
	if !s.verifyPayUResponseHash(params) {
		logger.Error("PayU hash verification failed for txn %s", params["txnid"])
		return "", fmt.Errorf("hash verification failed")
	}

	txnID := params["txnid"]
	userID := params["udf1"]
	plan := params["udf2"]
	mihpayID := params["mihpayid"]

	logger.Success("Payment verified: txn=%s user=%s plan=%s mihpayid=%s", txnID, userID, plan, mihpayID)

	// Update payment record
	_, err := s.db.Payments().UpdateOne(ctx,
		bson.M{"txn_id": txnID},
		bson.M{"$set": bson.M{
			"status":        "success",
			"mihpay_id":     mihpayID,
			"payu_response": fmt.Sprintf("status=%s&mihpayid=%s", params["status"], mihpayID),
		}},
	)
	if err != nil {
		logger.Error("Failed to update payment record: %v", err)
	}

	// Activate subscription
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return "", fmt.Errorf("invalid user ID in callback")
	}

	duration, ok := planDuration[plan]
	if !ok {
		duration = 30 * 24 * time.Hour
	}

	now := time.Now()
	opts := options.Replace().SetUpsert(true)
	_, err = s.db.Subscriptions().ReplaceOne(ctx,
		bson.M{"user_id": oid},
		subscriptionDoc{
			UserID:     oid,
			Plan:       plan,
			Status:     "active",
			StartDate:  now,
			ExpiryDate: now.Add(duration),
			CreatedAt:  now,
		},
		opts,
	)
	if err != nil {
		logger.Error("Failed to activate subscription: %v", err)
		return "", fmt.Errorf("failed to activate subscription: %w", err)
	}

	logger.Success("Subscription activated: user=%s plan=%s expires=%s", userID, plan, now.Add(duration).Format("2006-01-02"))

	// Send invoice email (non-blocking)
	if s.emailSvc != nil {
		go func() {
			var userDoc struct {
				Email string `bson:"email"`
				Name  string `bson:"name"`
			}
			if err := s.db.Users().FindOne(ctx, bson.M{"_id": oid}).Decode(&userDoc); err == nil {
				amount, _ := planPricing[plan]
				s.emailSvc.SendInvoiceEmail(InvoiceData{
					UserName:    userDoc.Name,
					UserEmail:   userDoc.Email,
					Plan:        plan,
					Amount:      fmt.Sprintf("%.2f", amount),
					TxnID:       txnID,
					PaymentID:   mihpayID,
					PaymentDate: now,
					ExpiryDate:  now.Add(duration),
				})
			}
		}()
	}

	return txnID, nil
}

// HandlePaymentFailure processes a failed PayU callback.
func (s *SubscriptionService) HandlePaymentFailure(ctx context.Context, params map[string]string) (string, error) {
	txnID := params["txnid"]
	logger.Warn("Payment failed: txn=%s reason=%s", txnID, params["field9"])

	// Update payment record
	_, err := s.db.Payments().UpdateOne(ctx,
		bson.M{"txn_id": txnID},
		bson.M{"$set": bson.M{
			"status":        "failure",
			"payu_response": fmt.Sprintf("status=%s&error=%s", params["status"], params["field9"]),
		}},
	)
	if err != nil {
		logger.Error("Failed to update payment record: %v", err)
	}

	return txnID, nil
}

// GetPaymentHistory returns all payments for a user.
func (s *SubscriptionService) GetPaymentHistory(ctx context.Context, userID string) ([]types.Payment, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}

	cursor, err := s.db.Payments().Find(ctx,
		bson.M{"user_id": oid},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}),
	)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []paymentDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	payments := make([]types.Payment, len(docs))
	for i, d := range docs {
		payments[i] = types.Payment{
			ID:        d.ID.Hex(),
			UserID:    d.UserID.Hex(),
			TxnID:     d.TxnID,
			Amount:    d.Amount,
			Plan:      d.Plan,
			Status:    d.Status,
			MihpayID:  d.MihpayID,
			CreatedAt: d.CreatedAt.Format(time.RFC3339),
		}
	}
	return payments, nil
}

// GetFrontendURL returns the frontend URL for redirects.
func (s *SubscriptionService) GetFrontendURL() string {
	return s.frontendURL
}

// PayU hash generation
// Formula: sha512(key|txnid|amount|productinfo|firstname|email|udf1|udf2|udf3|udf4|udf5||||||SALT)
func generatePayUHash(key, txnid, amount, productinfo, firstname, email, udf1, udf2, udf3, udf4, udf5, salt string) string {
	hashString := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s||||||%s",
		key, txnid, amount, productinfo, firstname, email,
		udf1, udf2, udf3, udf4, udf5, salt)
	h := sha512.New()
	h.Write([]byte(hashString))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// PayU response hash verification
// Reverse: sha512(SALT|status||||||udf5|udf4|udf3|udf2|udf1|email|firstname|productinfo|amount|txnid|key)
func (s *SubscriptionService) verifyPayUResponseHash(params map[string]string) bool {
	reverseHashString := fmt.Sprintf("%s|%s||||||%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		s.salt,
		params["status"],
		params["udf5"], params["udf4"], params["udf3"], params["udf2"], params["udf1"],
		params["email"], params["firstname"], params["productinfo"],
		params["amount"], params["txnid"], s.merchantKey)
	h := sha512.New()
	h.Write([]byte(reverseHashString))
	computed := fmt.Sprintf("%x", h.Sum(nil))
	return strings.EqualFold(computed, params["hash"])
}

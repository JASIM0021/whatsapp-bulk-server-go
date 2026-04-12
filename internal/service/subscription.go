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
const FreeMessageLimit = 5

type invoiceDoc struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	InvoiceNumber  string             `bson:"invoice_number"`
	UserID         primitive.ObjectID `bson:"user_id"`
	UserName       string             `bson:"user_name"`
	UserEmail      string             `bson:"user_email"`
	Plan           string             `bson:"plan"`
	OriginalAmount float64            `bson:"original_amount"`
	FinalAmount    float64            `bson:"final_amount"`
	TxnID          string             `bson:"txn_id"`
	MihpayID       string             `bson:"mihpay_id"`
	Status         string             `bson:"status"`
	PaymentDate    time.Time          `bson:"payment_date"`
	ExpiryDate     time.Time          `bson:"expiry_date"`
	CreatedAt      time.Time          `bson:"created_at"`
	SentAt         *time.Time         `bson:"sent_at,omitempty"`
}

type planConfigDoc struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	Plan         string             `bson:"plan"`
	Amount       float64            `bson:"amount"`
	MessageLimit int                `bson:"message_limit"`
	UpdatedAt    time.Time          `bson:"updated_at"`
}

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
	// Free trial: no time expiry, limited by message count (5 messages)
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
	amount, _, err := s.GetPlanConfig(ctx, plan)
	if err != nil || amount == 0 {
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

	// Create pending invoice record (non-fatal if fails)
	func() {
		var payDoc paymentDoc
		if fetchErr := s.db.Payments().FindOne(ctx, bson.M{"txn_id": txnID}).Decode(&payDoc); fetchErr != nil {
			logger.Error("Invoice creation: failed to fetch payment doc: %v", fetchErr)
			return
		}
		var userDoc struct {
			Email string `bson:"email"`
			Name  string `bson:"name"`
		}
		if fetchErr := s.db.Users().FindOne(ctx, bson.M{"_id": oid}).Decode(&userDoc); fetchErr != nil {
			logger.Error("Invoice creation: failed to fetch user doc: %v", fetchErr)
			return
		}
		invID := primitive.NewObjectID()
		hexStr := invID.Hex()
		invDoc := invoiceDoc{
			ID:             invID,
			InvoiceNumber:  fmt.Sprintf("INV-%s-%s", now.Format("200601"), hexStr[len(hexStr)-6:]),
			UserID:         oid,
			UserName:       userDoc.Name,
			UserEmail:      userDoc.Email,
			Plan:           plan,
			OriginalAmount: payDoc.Amount,
			FinalAmount:    payDoc.Amount,
			TxnID:          txnID,
			MihpayID:       mihpayID,
			Status:         "pending",
			PaymentDate:    now,
			ExpiryDate:     now.Add(duration),
			CreatedAt:      now,
		}
		if _, insertErr := s.db.Invoices().InsertOne(ctx, invDoc); insertErr != nil {
			logger.Error("Failed to create invoice: %v", insertErr)
		} else {
			logger.Info("Invoice created: %s for user %s", invDoc.InvoiceNumber, oid.Hex())
		}
	}()

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

// GetPlanConfig returns pricing and message limit for a plan, falling back to defaults.
func (s *SubscriptionService) GetPlanConfig(ctx context.Context, plan string) (amount float64, messageLimit int, err error) {
	var doc planConfigDoc
	fetchErr := s.db.PlanConfigs().FindOne(ctx, bson.M{"plan": plan}).Decode(&doc)
	if fetchErr == nil {
		return doc.Amount, doc.MessageLimit, nil
	}
	if fetchErr != mongo.ErrNoDocuments {
		return 0, 0, fmt.Errorf("failed to query plan config: %w", fetchErr)
	}
	// Fall back to hardcoded defaults
	switch plan {
	case "monthly":
		return planPricing["monthly"], 0, nil
	case "yearly":
		return planPricing["yearly"], 0, nil
	case "free":
		return 0, FreeMessageLimit, nil
	default:
		return 0, 0, fmt.Errorf("invalid plan: %s", plan)
	}
}

// GetPublicPlanPricing returns pricing info for all plans.
func (s *SubscriptionService) GetPublicPlanPricing(ctx context.Context) map[string]interface{} {
	result := make(map[string]interface{})
	for _, plan := range []string{"free", "monthly", "yearly"} {
		amount, messageLimit, _ := s.GetPlanConfig(ctx, plan)
		result[plan] = map[string]interface{}{
			"amount":       amount,
			"messageLimit": messageLimit,
		}
	}
	return result
}

// UpdatePlanConfig upserts pricing configuration for a plan.
func (s *SubscriptionService) UpdatePlanConfig(ctx context.Context, plan string, amount float64, messageLimit int) error {
	opts := options.Replace().SetUpsert(true)
	_, err := s.db.PlanConfigs().ReplaceOne(ctx,
		bson.M{"plan": plan},
		planConfigDoc{
			Plan:         plan,
			Amount:       amount,
			MessageLimit: messageLimit,
			UpdatedAt:    time.Now(),
		},
		opts,
	)
	return err
}

// ListInvoices returns paginated invoices filtered by status.
func (s *SubscriptionService) ListInvoices(ctx context.Context, status string, page, limit int) ([]types.Invoice, int64, error) {
	filter := bson.M{}
	if status != "" && status != "all" {
		filter["status"] = status
	}

	total, err := s.db.Invoices().CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	skip := int64((page - 1) * limit)
	cursor, err := s.db.Invoices().Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(skip).
		SetLimit(int64(limit)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var docs []invoiceDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}

	invoices := make([]types.Invoice, len(docs))
	for i, d := range docs {
		invoices[i] = invoiceDocToAPI(d)
	}
	return invoices, total, nil
}

// UpdateInvoiceAmount updates the final_amount of a pending invoice.
func (s *SubscriptionService) UpdateInvoiceAmount(ctx context.Context, invoiceID string, finalAmount float64) error {
	oid, err := primitive.ObjectIDFromHex(invoiceID)
	if err != nil {
		return fmt.Errorf("invalid invoice ID")
	}
	result, err := s.db.Invoices().UpdateOne(ctx,
		bson.M{"_id": oid, "status": "pending"},
		bson.M{"$set": bson.M{"final_amount": finalAmount}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("invoice not found or not pending")
	}
	return nil
}

// ApproveAndSendInvoice sends the invoice email and marks it as sent.
func (s *SubscriptionService) ApproveAndSendInvoice(ctx context.Context, invoiceID string, overrideAmount float64) error {
	oid, err := primitive.ObjectIDFromHex(invoiceID)
	if err != nil {
		return fmt.Errorf("invalid invoice ID")
	}

	var doc invoiceDoc
	if err := s.db.Invoices().FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); err != nil {
		return fmt.Errorf("invoice not found")
	}

	if doc.Status == "sent" {
		return fmt.Errorf("invoice already sent")
	}

	if s.emailSvc == nil {
		return fmt.Errorf("email service not configured")
	}

	finalAmount := doc.FinalAmount
	if overrideAmount > 0 {
		finalAmount = overrideAmount
	}

	if err := s.emailSvc.SendInvoiceEmail(InvoiceData{
		UserName:    doc.UserName,
		UserEmail:   doc.UserEmail,
		Plan:        doc.Plan,
		Amount:      fmt.Sprintf("%.2f", finalAmount),
		TxnID:       doc.TxnID,
		PaymentID:   doc.MihpayID,
		PaymentDate: doc.PaymentDate,
		ExpiryDate:  doc.ExpiryDate,
	}); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	now := time.Now()
	_, err = s.db.Invoices().UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
		"status":       "sent",
		"sent_at":      now,
		"final_amount": finalAmount,
	}})
	return err
}

func invoiceDocToAPI(d invoiceDoc) types.Invoice {
	sentAt := ""
	if d.SentAt != nil {
		sentAt = d.SentAt.Format(time.RFC3339)
	}
	return types.Invoice{
		ID:             d.ID.Hex(),
		InvoiceNumber:  d.InvoiceNumber,
		UserID:         d.UserID.Hex(),
		UserName:       d.UserName,
		UserEmail:      d.UserEmail,
		Plan:           d.Plan,
		OriginalAmount: d.OriginalAmount,
		FinalAmount:    d.FinalAmount,
		TxnID:          d.TxnID,
		MihpayID:       d.MihpayID,
		PaymentDate:    d.PaymentDate.Format(time.RFC3339),
		ExpiryDate:     d.ExpiryDate.Format("2006-01-02"),
		Status:         d.Status,
		CreatedAt:      d.CreatedAt.Format(time.RFC3339),
		SentAt:         sentAt,
	}
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

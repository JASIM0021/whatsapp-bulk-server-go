package service

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

type AuthService struct {
	db       *db.DB
	subSvc   *SubscriptionService
	emailSvc *EmailService
}

func NewAuthService(database *db.DB) *AuthService {
	return &AuthService{db: database}
}

// SetSubscriptionService sets the subscription service reference (avoids circular init).
func (s *AuthService) SetSubscriptionService(subSvc *SubscriptionService) {
	s.subSvc = subSvc
}

// SetEmailService sets the email service reference.
func (s *AuthService) SetEmailService(emailSvc *EmailService) {
	s.emailSvc = emailSvc
}

// resetDoc stores a pending password-reset OTP.
type resetDoc struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Email     string             `bson:"email"`
	Code      string             `bson:"code"`
	ExpiresAt time.Time          `bson:"expires_at"`
	CreatedAt time.Time          `bson:"created_at"`
}

// otpDoc stores a pending registration with a one-time verification code.
type otpDoc struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	Email          string             `bson:"email"`
	Name           string             `bson:"name"`
	HashedPassword string             `bson:"hashed_password"`
	Code           string             `bson:"code"`
	ExpiresAt      time.Time          `bson:"expires_at"`
	CreatedAt      time.Time          `bson:"created_at"`
}

// userDoc is the MongoDB document shape for the users collection.
type userDoc struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Email     string             `bson:"email"`
	Password  string             `bson:"password"`
	Name      string             `bson:"name"`
	Role      string             `bson:"role"`
	Status    string             `bson:"status,omitempty"`
	CreatedAt time.Time          `bson:"created_at"`
}

func (s *AuthService) Register(ctx context.Context, req types.RegisterRequest) (*types.AuthResponse, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	doc := userDoc{
		ID:        primitive.NewObjectID(),
		Email:     req.Email,
		Password:  string(hash),
		Name:      req.Name,
		Role:      "user",
		CreatedAt: time.Now(),
	}
	if _, err := s.db.Users().InsertOne(ctx, doc); err != nil {
		// Duplicate key = email already registered
		return nil, fmt.Errorf("email already registered")
	}

	userID := doc.ID.Hex()

	// Seed default templates (non-fatal)
	templateSvc := NewTemplateService(s.db)
	_ = templateSvc.seedDefaultTemplates(ctx, userID)

	// Create free trial subscription with 50 message limit (non-fatal)
	if s.subSvc != nil {
		_ = s.subSvc.CreateTrialSubscription(ctx, userID)
	}

	token, err := s.GenerateToken(userID, req.Email, "user")
	if err != nil {
		return nil, err
	}

	userInfo := types.UserInfo{ID: userID, Email: req.Email, Name: req.Name, Role: "user"}
	if s.subSvc != nil {
		subInfo, _ := s.subSvc.GetSubscription(ctx, userID)
		userInfo.Subscription = subInfo
	}

	// Send welcome email (non-blocking)
	if s.emailSvc != nil {
		go s.emailSvc.SendWelcomeEmail(req.Email, req.Name)
	}

	return &types.AuthResponse{
		Token: token,
		User:  userInfo,
	}, nil
}

func (s *AuthService) Login(ctx context.Context, req types.LoginRequest) (*types.AuthResponse, error) {
	var doc userDoc
	err := s.db.Users().FindOne(ctx, bson.M{"email": req.Email}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("invalid email or password")
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(doc.Password), []byte(req.Password)); err != nil {
		return nil, fmt.Errorf("invalid email or password")
	}

	if doc.Status == "blocked" {
		return nil, fmt.Errorf("your account has been blocked. Contact admin for support")
	}

	userID := doc.ID.Hex()
	token, err := s.GenerateToken(userID, doc.Email, doc.Role)
	if err != nil {
		return nil, err
	}

	userInfo := types.UserInfo{ID: userID, Email: doc.Email, Name: doc.Name, Role: doc.Role}
	if s.subSvc != nil {
		subInfo, _ := s.subSvc.GetSubscription(ctx, userID)
		userInfo.Subscription = subInfo
	}

	return &types.AuthResponse{
		Token: token,
		User:  userInfo,
	}, nil
}

func (s *AuthService) GetUser(ctx context.Context, userID string) (*types.UserInfo, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}
	var doc userDoc
	if err := s.db.Users().FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); err != nil {
		return nil, fmt.Errorf("user not found")
	}
	userInfo := &types.UserInfo{ID: doc.ID.Hex(), Email: doc.Email, Name: doc.Name, Role: doc.Role}
	if s.subSvc != nil {
		subInfo, _ := s.subSvc.GetSubscription(ctx, doc.ID.Hex())
		userInfo.Subscription = subInfo
	}
	return userInfo, nil
}

// GetUserByEmail looks up a user by email and returns their info.
func (s *AuthService) GetUserByEmail(ctx context.Context, email string) (*types.UserInfo, error) {
	var doc userDoc
	if err := s.db.Users().FindOne(ctx, bson.M{"email": email}).Decode(&doc); err != nil {
		return nil, fmt.Errorf("user not found")
	}
	return &types.UserInfo{ID: doc.ID.Hex(), Email: doc.Email, Name: doc.Name, Role: doc.Role}, nil
}

func (s *AuthService) GenerateToken(userID, email, role string) (string, error) {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"role":  role,
		"exp":   time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret()))
}

func (s *AuthService) ValidateToken(tokenStr string) (string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(jwtSecret()), nil
	})
	if err != nil {
		return "", err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("invalid token")
	}

	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", fmt.Errorf("invalid token claims")
	}
	return sub, nil
}

// SendRegistrationOTP generates a 5-digit OTP, stores the pending registration,
// and emails the code. Existing pending OTP for that email is replaced.
func (s *AuthService) SendRegistrationOTP(ctx context.Context, req types.SendOTPRequest) error {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || req.Password == "" || req.Name == "" {
		return fmt.Errorf("email, name and password are required")
	}

	// Check if email already registered
	var existing userDoc
	err := s.db.Users().FindOne(ctx, bson.M{"email": email}).Decode(&existing)
	if err == nil {
		return fmt.Errorf("email already registered")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		return fmt.Errorf("failed to process password")
	}

	// 5-digit OTP
	code := fmt.Sprintf("%05d", rand.Intn(100000))
	now := time.Now()

	doc := otpDoc{
		ID:             primitive.NewObjectID(),
		Email:          email,
		Name:           strings.TrimSpace(req.Name),
		HashedPassword: string(hash),
		Code:           code,
		ExpiresAt:      now.Add(10 * time.Minute),
		CreatedAt:      now,
	}

	// Upsert: replace existing OTP for this email (handles resend)
	opts := options.Replace().SetUpsert(true)
	_, err = s.db.EmailOTPs().ReplaceOne(ctx, bson.M{"email": email}, doc, opts)
	if err != nil {
		return fmt.Errorf("failed to store OTP")
	}

	// Send email (blocking so caller knows if it failed)
	if s.emailSvc != nil {
		if err := s.emailSvc.SendOTPEmail(email, req.Name, code); err != nil {
			return fmt.Errorf("failed to send verification email: %w", err)
		}
	}
	return nil
}

// VerifyOTPAndRegister checks the OTP and, if valid, creates the user account.
func (s *AuthService) VerifyOTPAndRegister(ctx context.Context, req types.VerifyOTPRequest) (*types.AuthResponse, error) {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	code := strings.TrimSpace(req.Code)

	var doc otpDoc
	err := s.db.EmailOTPs().FindOne(ctx, bson.M{"email": email}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("no pending verification found for this email")
	}
	if err != nil {
		return nil, fmt.Errorf("verification failed")
	}
	if time.Now().After(doc.ExpiresAt) {
		s.db.EmailOTPs().DeleteOne(ctx, bson.M{"email": email})
		return nil, fmt.Errorf("verification code has expired. Please request a new one")
	}
	if doc.Code != code {
		return nil, fmt.Errorf("incorrect verification code")
	}

	// OTP valid — delete it and create the user
	s.db.EmailOTPs().DeleteOne(ctx, bson.M{"email": email})

	userID := primitive.NewObjectID()
	userDoc := userDoc{
		ID:        userID,
		Email:     email,
		Password:  doc.HashedPassword,
		Name:      doc.Name,
		Role:      "user",
		CreatedAt: time.Now(),
	}
	if _, err := s.db.Users().InsertOne(ctx, userDoc); err != nil {
		return nil, fmt.Errorf("email already registered")
	}

	userIDHex := userID.Hex()

	// Seed templates (non-fatal)
	templateSvc := NewTemplateService(s.db)
	_ = templateSvc.seedDefaultTemplates(ctx, userIDHex)

	// Create trial subscription (non-fatal)
	if s.subSvc != nil {
		_ = s.subSvc.CreateTrialSubscription(ctx, userIDHex)
	}

	token, err := s.GenerateToken(userIDHex, email, "user")
	if err != nil {
		return nil, err
	}

	userInfo := types.UserInfo{ID: userIDHex, Email: email, Name: doc.Name, Role: "user"}
	if s.subSvc != nil {
		subInfo, _ := s.subSvc.GetSubscription(ctx, userIDHex)
		userInfo.Subscription = subInfo
	}

	// Send welcome email (non-blocking)
	if s.emailSvc != nil {
		go s.emailSvc.SendWelcomeEmail(email, doc.Name)
	}

	return &types.AuthResponse{Token: token, User: userInfo}, nil
}

// SendPasswordResetOTP generates a 5-digit OTP and emails it to the user.
// If the email doesn't exist we return success anyway (don't leak whether email is registered).
func (s *AuthService) SendPasswordResetOTP(ctx context.Context, req types.ForgotPasswordRequest) error {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		return fmt.Errorf("email is required")
	}

	// Verify the email is actually registered
	var u userDoc
	err := s.db.Users().FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if err == mongo.ErrNoDocuments {
		// Return nil so we don't expose whether the email exists
		return nil
	}
	if err != nil {
		return fmt.Errorf("database error")
	}

	code := fmt.Sprintf("%05d", rand.Intn(100000))
	now := time.Now()

	doc := resetDoc{
		ID:        primitive.NewObjectID(),
		Email:     email,
		Code:      code,
		ExpiresAt: now.Add(10 * time.Minute),
		CreatedAt: now,
	}

	opts := options.Replace().SetUpsert(true)
	if _, err := s.db.PasswordResets().ReplaceOne(ctx, bson.M{"email": email}, doc, opts); err != nil {
		return fmt.Errorf("failed to store reset code")
	}

	if s.emailSvc != nil {
		if err := s.emailSvc.SendPasswordResetEmail(email, u.Name, code); err != nil {
			return fmt.Errorf("failed to send reset email: %w", err)
		}
	}
	return nil
}

// ResetPassword verifies the OTP and updates the user's password.
func (s *AuthService) ResetPassword(ctx context.Context, req types.ResetPasswordRequest) error {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	code := strings.TrimSpace(req.Code)

	if len(req.NewPassword) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}

	var doc resetDoc
	err := s.db.PasswordResets().FindOne(ctx, bson.M{"email": email}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return fmt.Errorf("no password reset request found. Please request a new code")
	}
	if err != nil {
		return fmt.Errorf("verification failed")
	}
	if time.Now().After(doc.ExpiresAt) {
		s.db.PasswordResets().DeleteOne(ctx, bson.M{"email": email})
		return fmt.Errorf("reset code has expired. Please request a new one")
	}
	if doc.Code != code {
		return fmt.Errorf("incorrect reset code")
	}

	// Delete used OTP
	s.db.PasswordResets().DeleteOne(ctx, bson.M{"email": email})

	// Hash new password and update user
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
	if err != nil {
		return fmt.Errorf("failed to process password")
	}
	_, err = s.db.Users().UpdateOne(ctx,
		bson.M{"email": email},
		bson.M{"$set": bson.M{"password": string(hash)}},
	)
	if err != nil {
		return fmt.Errorf("failed to update password")
	}
	return nil
}

func jwtSecret() string {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return s
	}
	return "whatsapp-bulk-secret-key-change-in-production"
}

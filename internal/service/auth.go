package service

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

type AuthService struct {
	db     *db.DB
	subSvc *SubscriptionService
}

func NewAuthService(database *db.DB) *AuthService {
	return &AuthService{db: database}
}

// SetSubscriptionService sets the subscription service reference (avoids circular init).
func (s *AuthService) SetSubscriptionService(subSvc *SubscriptionService) {
	s.subSvc = subSvc
}

// userDoc is the MongoDB document shape for the users collection.
type userDoc struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Email     string             `bson:"email"`
	Password  string             `bson:"password"`
	Name      string             `bson:"name"`
	Role      string             `bson:"role"`
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

	// Create free 7-day trial subscription (non-fatal)
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

func jwtSecret() string {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return s
	}
	return "whatsapp-bulk-secret-key-change-in-production"
}

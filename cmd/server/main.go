package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/handler"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/logger"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/middleware"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
	"github.com/joho/godotenv"
)

func main() {
	logger.Section("WHATSAPP BULK SENDER - GO BACKEND")
	logger.Info("Starting server initialization...")

	// Load environment variables
	if err := godotenv.Load(); err != nil {
		logger.Warn("No .env file found, using system environment variables")
	} else {
		logger.Success("Loaded configuration from .env file")
	}

	// Initialize app database
	logger.Info("Connecting to MongoDB...")
	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = os.Getenv("MONGODB_URL")
	}
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	appDB, err := db.New(mongoURI)
	if err != nil {
		logger.Error("Failed to connect to MongoDB: %v", err)
		os.Exit(1)
	}
	if err := appDB.EnsureIndexes(context.Background()); err != nil {
		logger.Error("Failed to ensure indexes: %v", err)
		os.Exit(1)
	}
	logger.Success("MongoDB connected and ready")

	// Initialize services first (needed for seeding)
	authService := service.NewAuthService(appDB)
	subscriptionService := service.NewSubscriptionService(appDB)
	emailService := service.NewEmailService()
	authService.SetSubscriptionService(subscriptionService)
	authService.SetEmailService(emailService)
	subscriptionService.SetEmailService(emailService)
	templateService := service.NewTemplateService(appDB)

	// Seed default user if not exists
	logger.Info("Checking for default user...")
	seedUser := types.RegisterRequest{
		Email:    "user1@gmail.com",
		Password: "123456",
		Name:     "Test User",
	}
	userResp, err := authService.Register(context.Background(), seedUser)
	if err != nil {
		logger.Info("Default user already exists")
		// Ensure existing users have a trial subscription — reset if expired
		existingUser, _ := authService.GetUserByEmail(context.Background(), "user1@gmail.com")
		if existingUser != nil {
			_ = subscriptionService.EnsureTrialSubscription(context.Background(), existingUser.ID)
		}
	} else {
		logger.Success("Created default user: user1@gmail.com / 123456")
	}

	// Seed default templates
	logger.Info("Seeding default templates...")
	defaultTemplates := []types.CreateTemplateRequest{
		{Name: "Welcome Message", Category: "Marketing", Body: "Hello {{name}}! Welcome to our service. We're glad to have you!", Variables: []string{"name"}},
		{Name: "Order Confirmation", Category: "Utility", Body: "Hi {{name}}, your order #{{order_id}} has been confirmed. Expected delivery: {{delivery_date}}", Variables: []string{"name", "order_id", "delivery_date"}},
		{Name: "Payment Reminder", Category: "Utility", Body: "Hello {{name}}, this is a reminder that your payment of {{amount}} is due on {{due_date}}", Variables: []string{"name", "amount", "due_date"}},
		{Name: "OTP Verification", Category: "Authentication", Body: "Your verification code is: {{otp}}. Valid for {{validity}} minutes.", Variables: []string{"name", "otp", "validity"}},
		{Name: "Promotional Offer", Category: "Marketing", Body: "Hi {{name}}! 🎉 Get {{discount}}% off on your next purchase. Use code: {{code}}", Variables: []string{"name", "discount", "code"}},
	}
	for _, tmpl := range defaultTemplates {
		if userResp != nil {
			templateService.Create(context.Background(), userResp.User.ID, tmpl)
		}
	}
	logger.Success("Default templates created")

	// Initialize handlers
	authHandler := handler.NewAuthHandler(authService)
	templateHandler := handler.NewTemplateHandler(templateService)
	subscriptionHandler := handler.NewSubscriptionHandler(subscriptionService, authService)
	whatsappHandler := handler.NewWhatsAppHandler("sessions", appDB)
	uploadHandler := handler.NewUploadHandler()
	imageHandler := handler.NewImageHandler()
	adminHandler := handler.NewAdminHandler(appDB.MongoDB(), authService, emailService)

	// Auth middleware helper
	authMiddleware := middleware.Auth(authService)
	subMiddleware := middleware.RequireSubscription(subscriptionService)
	wrap := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(http.HandlerFunc(h))
	}
	// wrapSub applies auth + subscription check
	wrapSub := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(subMiddleware(http.HandlerFunc(h)))
	}

	// Create HTTP mux
	mux := http.NewServeMux()

	// Public routes
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"message":"Server is running"}`))
	})
	mux.HandleFunc("/api/auth/register", authHandler.Register)
	mux.HandleFunc("/api/auth/login", authHandler.Login)

	// PayU callback routes (PUBLIC — no JWT, security via hash verification)
	mux.HandleFunc("/api/payment/success", subscriptionHandler.PaymentSuccess)
	mux.HandleFunc("/api/payment/failure", subscriptionHandler.PaymentFailure)

	// Protected routes (auth only)
	mux.Handle("/api/auth/me", wrap(authHandler.Me))
	mux.Handle("/api/subscription", wrap(subscriptionHandler.GetSubscription))
	mux.Handle("/api/payment/initiate", wrap(subscriptionHandler.InitiatePayment))
	mux.Handle("/api/payment/history", wrap(subscriptionHandler.GetPaymentHistory))

	// Protected routes (auth + active subscription required)
	mux.Handle("/api/whatsapp/init", wrapSub(whatsappHandler.Initialize))
	mux.Handle("/api/whatsapp/qr", wrapSub(whatsappHandler.GetQRCode))
	mux.Handle("/api/whatsapp/status", wrap(whatsappHandler.GetStatus)) // status check doesn't need subscription
	mux.Handle("/api/whatsapp/disconnect", wrap(whatsappHandler.Disconnect)) // allow disconnect always
	mux.Handle("/api/whatsapp/send", wrapSub(whatsappHandler.SendMessages))
	mux.Handle("/api/upload", wrapSub(uploadHandler.UploadFile))
	mux.Handle("/api/upload/image", wrapSub(imageHandler.UploadImage))
	mux.Handle("/api/templates", wrapSub(templateHandler.HandleCollection))
	mux.Handle("/api/templates/", wrapSub(templateHandler.Single))

	// Admin routes (auth + admin role required)
	adminMiddleware := middleware.AdminOnly(appDB.MongoDB())
	wrapAdmin := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(adminMiddleware(http.HandlerFunc(h)))
	}
	mux.Handle("/api/admin/stats", wrapAdmin(adminHandler.GetStats))
	mux.Handle("/api/admin/users", wrapAdmin(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.ListUsers(w, r)
		case http.MethodPost:
			adminHandler.CreateUser(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	mux.Handle("/api/admin/users/", wrapAdmin(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.GetUser(w, r)
		case http.MethodPut:
			adminHandler.UpdateUser(w, r)
		case http.MethodDelete:
			adminHandler.DeleteUser(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	mux.Handle("/api/admin/email/promotional", wrapAdmin(adminHandler.SendPromotionalEmail))

	logger.Info("Registered API routes:")
	logger.Info("  • GET  /api/health")
	logger.Info("  • POST /api/auth/register")
	logger.Info("  • POST /api/auth/login")
	logger.Info("  • GET  /api/auth/me  [protected]")
	logger.Info("  • GET  /api/subscription  [protected]")
	logger.Info("  • POST /api/payment/initiate  [protected]")
	logger.Info("  • POST /api/payment/success  [public - PayU callback]")
	logger.Info("  • POST /api/payment/failure  [public - PayU callback]")
	logger.Info("  • GET  /api/payment/history  [protected]")
	logger.Info("  • POST /api/whatsapp/init  [protected+subscription]")
	logger.Info("  • GET  /api/whatsapp/qr  [protected+subscription]")
	logger.Info("  • GET  /api/whatsapp/status  [protected+subscription]")
	logger.Info("  • POST /api/whatsapp/disconnect  [protected+subscription]")
	logger.Info("  • POST /api/whatsapp/send  [protected+subscription]")
	logger.Info("  • POST /api/upload  [protected+subscription]")
	logger.Info("  • POST /api/upload/image  [protected+subscription]")
	logger.Info("  • GET/POST /api/templates  [protected+subscription]")
	logger.Info("  • PUT/DELETE /api/templates/{id}  [protected+subscription]")
	logger.Info("  • GET  /api/admin/stats  [admin]")
	logger.Info("  • GET  /api/admin/users  [admin]")
	logger.Info("  • GET/PUT/DELETE /api/admin/users/{id}  [admin]")
	logger.Info("  • POST /api/admin/email/promotional  [admin]")

	// Apply CORS + Logging middleware
	handlerWithMiddleware := middleware.Logging(middleware.CORS(mux))

	// Server configuration
	port := os.Getenv("PORT")
	if port == "" {
		port = "4000"
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      handlerWithMiddleware,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // No write timeout for SSE streaming
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Section("SERVER STARTED")
		logger.Success("🚀 Server listening on http://localhost:%s", port)
		logger.Info("Frontend URL: %s", os.Getenv("FRONTEND_URL"))
		logger.Info("Press CTRL+C to stop")
		logger.Section("")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server error: %v", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Section("SHUTTING DOWN")
	logger.Info("Received shutdown signal...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Disconnect all WhatsApp sessions
	logger.Info("Disconnecting all WhatsApp sessions...")
	whatsappHandler.Shutdown()
	logger.Success("All WhatsApp sessions disconnected")

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown: %v", err)
		os.Exit(1)
	}

	logger.Success("Server stopped gracefully")
	logger.Section("GOODBYE")
}

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/handler"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/logger"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/middleware"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/joho/godotenv"
)

func main() {
	// Print startup banner
	logger.Section("WHATSAPP BULK SENDER - GO BACKEND")
	logger.Info("Starting server initialization...")

	// Load environment variables
	if err := godotenv.Load(); err != nil {
		logger.Warn("No .env file found, using system environment variables")
	} else {
		logger.Success("Loaded configuration from .env file")
	}

	// Initialize WhatsApp service
	logger.Info("Initializing WhatsApp service...")
	waService, err := service.NewWhatsAppService()
	if err != nil {
		logger.Error("Failed to initialize WhatsApp service: %v", err)
		os.Exit(1)
	}
	logger.Success("WhatsApp service initialized successfully")

	// Create HTTP server
	mux := http.NewServeMux()

	// Initialize handlers
	whatsappHandler := handler.NewWhatsAppHandler(waService)
	uploadHandler := handler.NewUploadHandler()
	imageHandler := handler.NewImageHandler()

	// Health check
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"message":"Server is running"}`))
	})

	// WhatsApp routes
	mux.HandleFunc("/api/whatsapp/init", whatsappHandler.Initialize)
	mux.HandleFunc("/api/whatsapp/qr", whatsappHandler.GetQRCode)
	mux.HandleFunc("/api/whatsapp/status", whatsappHandler.GetStatus)
	mux.HandleFunc("/api/whatsapp/disconnect", whatsappHandler.Disconnect)
	mux.HandleFunc("/api/whatsapp/send", whatsappHandler.SendMessages)

	// Upload routes
	mux.HandleFunc("/api/upload", uploadHandler.UploadFile)
	mux.HandleFunc("/api/upload/image", imageHandler.UploadImage)

	logger.Info("Registered API routes:")
	logger.Info("  • GET  /api/health")
	logger.Info("  • POST /api/whatsapp/init")
	logger.Info("  • GET  /api/whatsapp/qr")
	logger.Info("  • GET  /api/whatsapp/status")
	logger.Info("  • POST /api/whatsapp/disconnect")
	logger.Info("  • POST /api/whatsapp/send")
	logger.Info("  • POST /api/upload")
	logger.Info("  • POST /api/upload/image")

	// Apply middleware (CORS + Logging)
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

	// Start server in goroutine
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

	// Disconnect WhatsApp
	logger.Info("Disconnecting WhatsApp client...")
	waService.Disconnect()
	logger.Success("WhatsApp client disconnected")

	// Shutdown HTTP server
	logger.Info("Stopping HTTP server...")
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown: %v", err)
		os.Exit(1)
	}

	logger.Success("Server stopped gracefully")
	logger.Section("GOODBYE")
}

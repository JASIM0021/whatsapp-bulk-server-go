package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/logger"
)

type WhatsAppService struct {
	client         *whatsmeow.Client
	container      *sqlstore.Container
	qrChan         chan string
	readyChan      chan bool
	disconnectChan chan string
	mu             sync.RWMutex
	isReady        bool
	lastQR         string
}

func NewWhatsAppService() (*WhatsAppService, error) {
	dbPath := os.Getenv("WHATSAPP_SESSION_PATH")
	if dbPath == "" {
		dbPath = "./whatsapp_session.db"
	}

	// Create database logger
	dbLog := waLog.Stdout("Database", "INFO", true)
	container, err := sqlstore.New(context.Background(), "sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), dbLog)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	service := &WhatsAppService{
		container:      container,
		qrChan:         make(chan string, 10),
		readyChan:      make(chan bool, 10),
		disconnectChan: make(chan string, 10),
	}

	return service, nil
}

func (s *WhatsAppService) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil && s.client.IsConnected() {
		logger.Info("WhatsApp client already connected")
		return nil
	}

	logger.Info("Initializing WhatsApp client...")

	// Get first device or create new
	deviceStore, err := s.container.GetFirstDevice(context.Background())
	if err != nil {
		logger.Error("Failed to get device from store: %v", err)
		return fmt.Errorf("failed to get device: %w", err)
	}

	// Check if we have a valid session
	if deviceStore.ID != nil {
		logger.Info("Found existing session in database (Device ID: *****%s)", deviceStore.ID.String()[len(deviceStore.ID.String())-8:])
	} else {
		logger.Info("No existing session found - will generate QR code for new login")
	}

	// Create client logger
	clientLog := waLog.Stdout("WhatsApp", "INFO", true)
	s.client = whatsmeow.NewClient(deviceStore, clientLog)

	// Register event handlers
	s.client.AddEventHandler(s.handleEvents)

	// Connect
	if s.client.Store.ID == nil {
		logger.Info("No existing session found. Generating QR code...")

		// No session exists, need to login
		qrChan, err := s.client.GetQRChannel(context.Background())
		if err != nil {
			logger.Error("Failed to get QR channel: %v", err)
			return fmt.Errorf("failed to get QR channel: %w", err)
		}

		err = s.client.Connect()
		if err != nil {
			logger.Error("Failed to connect to WhatsApp: %v", err)
			return fmt.Errorf("failed to connect: %w", err)
		}

		// Handle QR codes in background
		go func() {
			for evt := range qrChan {
				if evt.Event == "code" {
					logger.Info("New QR code generated")
					qrCode, err := s.generateQRDataURL(evt.Code)
					if err == nil {
						s.lastQR = qrCode
						select {
						case s.qrChan <- qrCode:
						default:
						}
					}
				}
			}
		}()
	} else {
		logger.Success("Existing session found! Connecting...")

		// Session exists, just connect
		err = s.client.Connect()
		if err != nil {
			logger.Error("Failed to connect with existing session: %v", err)
			return fmt.Errorf("failed to connect: %w", err)
		}
		s.isReady = true
		select {
		case s.readyChan <- true:
		default:
		}

		logger.Success("Connected successfully with existing session!")
	}

	return nil
}

func (s *WhatsAppService) handleEvents(evt interface{}) {
	switch evt.(type) {
	case *events.LoggedOut:
		s.mu.Lock()
		s.isReady = false
		s.mu.Unlock()
		select {
		case s.disconnectChan <- "logged out":
		default:
		}
	case *events.Connected:
		if s.client.Store.ID != nil {
			s.mu.Lock()
			s.isReady = true
			s.mu.Unlock()
			select {
			case s.readyChan <- true:
			default:
			}
		}
	case *events.StreamReplaced:
		s.mu.Lock()
		s.isReady = false
		s.mu.Unlock()
		select {
		case s.disconnectChan <- "disconnected":
		default:
		}
	}
}

func (s *WhatsAppService) generateQRDataURL(code string) (string, error) {
	png, err := qrcode.Encode(code, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(png)
	return fmt.Sprintf("data:image/png;base64,%s", encoded), nil
}

func (s *WhatsAppService) GetQRChannel() <-chan string {
	return s.qrChan
}

func (s *WhatsAppService) GetReadyChannel() <-chan bool {
	return s.readyChan
}

func (s *WhatsAppService) GetDisconnectChannel() <-chan string {
	return s.disconnectChan
}

func (s *WhatsAppService) GetStatus() (bool, bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	isConnected := s.client != nil && s.client.IsConnected()
	hasQR := s.lastQR != ""
	return isConnected, s.isReady, hasQR
}

func (s *WhatsAppService) IsReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isReady
}

func (s *WhatsAppService) SendMessage(phone, message string) error {
	s.mu.RLock()
	client := s.client
	ready := s.isReady
	s.mu.RUnlock()

	if !ready || client == nil {
		logger.Error("WhatsApp client not ready - isReady: %v, client nil: %v", ready, client == nil)
		return fmt.Errorf("WhatsApp client not ready")
	}

	// Parse and normalize phone number
	jid, normalizedPhone, err := s.parseAndNormalizePhone(phone)
	if err != nil {
		logger.Error("Invalid phone number '%s': %v", phone, err)
		return fmt.Errorf("invalid phone number: %w", err)
	}

	logger.Debug("Parsed phone: %s -> JID: %s", phone, jid.String())

	// CRITICAL: Check if the number is registered on WhatsApp
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	isOnWhatsAppResult, err := client.IsOnWhatsApp(ctx, []string{normalizedPhone})
	if err != nil {
		logger.Warn("IsOnWhatsApp check failed for %s: %v (will attempt to send anyway)", normalizedPhone, err)
	} else if len(isOnWhatsAppResult) > 0 {
		if !isOnWhatsAppResult[0].IsIn {
			logger.WhatsAppNotRegistered(phone)
			return fmt.Errorf("number %s is not registered on WhatsApp", phone)
		}
		// IMPORTANT: Use the JID returned by WhatsApp (this is the correct, verified JID)
		jid = isOnWhatsAppResult[0].JID
		logger.WhatsAppVerified(phone, jid.String())
	}

	// Prepare message preview for logging
	msgPreview := message
	if len(msgPreview) > 100 {
		msgPreview = msgPreview[:100] + "..."
	}
	logger.Debug("Sending message to %s: \"%s\"", jid.String(), msgPreview)

	// Send message with timeout
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()

	// Use Conversation field for better compatibility
	resp, err := client.SendMessage(ctx2, jid, &waProto.Message{
		Conversation: proto.String(message),
	})

	if err != nil {
		logger.Error("Failed to send message to %s (JID: %s): %v", phone, jid.String(), err)
		return fmt.Errorf("failed to send message: %w", err)
	}

	// Verify the response
	if resp.ID == "" {
		logger.Error("Message sent but no message ID returned for %s", phone)
		return fmt.Errorf("message sent but no confirmation received")
	}

	logger.Debug("Message sent successfully! ID: %s, Timestamp: %v, ServerID: %d", resp.ID, resp.Timestamp, resp.ServerID)
	return nil
}

func (s *WhatsAppService) parseAndNormalizePhone(phone string) (types.JID, string, error) {
	// Remove all non-digit characters except '+'
	cleanPhone := ""
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			cleanPhone += string(char)
		}
	}

	if cleanPhone == "" {
		return types.JID{}, "", fmt.Errorf("invalid phone number: no digits found")
	}

	// Ensure phone starts with country code
	// If it doesn't start with a valid international prefix, it might need one
	if len(cleanPhone) < 10 {
		return types.JID{}, "", fmt.Errorf("phone number too short: %s", cleanPhone)
	}

	// Remove leading zeros (common issue)
	cleanPhone = strings.TrimLeft(cleanPhone, "0")

	// Create normalized phone with + prefix for WhatsApp check
	normalizedPhone := "+" + cleanPhone

	// Create JID (without + symbol)
	jid := types.JID{
		User:   cleanPhone,
		Server: types.DefaultUserServer,
	}

	logger.Debug("Phone normalization: '%s' -> clean: '%s' -> normalized: '%s' -> JID: %s",
		phone, cleanPhone, normalizedPhone, jid.String())

	return jid, normalizedPhone, nil
}

func (s *WhatsAppService) Disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()

	logger.Info("Disconnecting WhatsApp client...")

	if s.client != nil {
		// Get device store before logout
		deviceStore := s.client.Store

		// Logout from WhatsApp (this tells WhatsApp servers to disconnect)
		err := s.client.Logout(context.Background())
		if err != nil {
			logger.Warn("Logout error (non-critical): %v", err)
		}

		// Disconnect the client
		s.client.Disconnect()

		// Delete the device/session from database to force new QR code on next login
		if deviceStore != nil && deviceStore.ID != nil {
			logger.Info("Deleting session from database to force new QR code...")
			err = deviceStore.Delete(context.Background())
			if err != nil {
				logger.Warn("Failed to delete session from database: %v", err)
			} else {
				logger.Success("Session deleted from database - next login will require new QR code")
			}
		}

		s.client = nil // Clear client reference
		s.isReady = false
		s.lastQR = "" // Clear last QR code
		logger.Success("WhatsApp client disconnected and logged out")
	}

	// Don't close the container - keep it open for reconnection
	logger.Info("Database container kept open for future connections")
}

// SendWithDelay sends a message with rate limiting
func (s *WhatsAppService) SendWithDelay(phone, message string, delay time.Duration) error {
	time.Sleep(delay)
	return s.SendMessage(phone, message)
}

// SendMessageWithImage sends a message with an image
func (s *WhatsAppService) SendMessageWithImage(phone, message, imagePath string) error {
	s.mu.RLock()
	client := s.client
	ready := s.isReady
	s.mu.RUnlock()

	if !ready || client == nil {
		logger.Error("WhatsApp client not ready - isReady: %v, client nil: %v", ready, client == nil)
		return fmt.Errorf("WhatsApp client not ready")
	}

	// Parse and normalize phone number
	jid, normalizedPhone, err := s.parseAndNormalizePhone(phone)
	if err != nil {
		logger.Error("Invalid phone number '%s': %v", phone, err)
		return fmt.Errorf("invalid phone number: %w", err)
	}

	logger.Debug("Parsed phone: %s -> JID: %s", phone, jid.String())

	// Check if the number is registered on WhatsApp
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	isOnWhatsAppResult, err := client.IsOnWhatsApp(ctx, []string{normalizedPhone})
	if err != nil {
		logger.Warn("IsOnWhatsApp check failed for %s: %v (will attempt to send anyway)", normalizedPhone, err)
	} else if len(isOnWhatsAppResult) > 0 {
		if !isOnWhatsAppResult[0].IsIn {
			logger.WhatsAppNotRegistered(phone)
			return fmt.Errorf("number %s is not registered on WhatsApp", phone)
		}
		jid = isOnWhatsAppResult[0].JID
		logger.WhatsAppVerified(phone, jid.String())
	}

	// Read image file
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		logger.Error("Failed to read image file '%s': %v", imagePath, err)
		return fmt.Errorf("failed to read image: %w", err)
	}

	// Detect MIME type
	mimeType := http.DetectContentType(imageData)
	logger.Debug("Detected image MIME type: %s", mimeType)

	// Upload image to WhatsApp servers
	logger.Info("Uploading image to WhatsApp servers... (size: %d bytes)", len(imageData))
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	uploaded, err := client.Upload(ctx2, imageData, whatsmeow.MediaImage)
	if err != nil {
		logger.Error("Failed to upload image to WhatsApp: %v", err)
		return fmt.Errorf("failed to upload image: %w", err)
	}

	logger.Success("Image uploaded successfully! URL: %s", uploaded.URL)

	// Prepare image message
	imageMsg := &waProto.ImageMessage{
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		Mimetype:      proto.String(mimeType),
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(imageData))),
	}

	// Add caption if message is provided
	if message != "" {
		imageMsg.Caption = proto.String(message)
	}

	// Send message
	ctx3, cancel3 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel3()

	resp, err := client.SendMessage(ctx3, jid, &waProto.Message{
		ImageMessage: imageMsg,
	})

	if err != nil {
		logger.Error("Failed to send image message to %s (JID: %s): %v", phone, jid.String(), err)
		return fmt.Errorf("failed to send image message: %w", err)
	}

	if resp.ID == "" {
		logger.Error("Image message sent but no message ID returned for %s", phone)
		return fmt.Errorf("message sent but no confirmation received")
	}

	logger.Debug("Image message sent successfully! ID: %s, Timestamp: %v", resp.ID, resp.Timestamp)
	return nil
}

// SendMessageWithImageURL sends a message with an image from URL
func (s *WhatsAppService) SendMessageWithImageURL(phone, message, imageURL string) error {
	// Download image from URL
	logger.Info("Downloading image from URL: %s", imageURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Error("Failed to download image from %s: %v", imageURL, err)
		return fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download image: HTTP %d", resp.StatusCode)
	}

	// Save to temporary file
	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("whatsapp_img_%d%s", time.Now().Unix(), filepath.Ext(imageURL)))
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer file.Close()
	defer os.Remove(tempFile) // Clean up after sending

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save image: %w", err)
	}

	logger.Success("Image downloaded to: %s", tempFile)

	// Send using the image file
	return s.SendMessageWithImage(phone, message, tempFile)
}

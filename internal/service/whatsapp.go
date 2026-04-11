package service

import (
	"context"
	"database/sql"
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
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/proto"

	waProto "go.mau.fi/whatsmeow/binary/proto"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/logger"
)

// waSessionDoc is the MongoDB document that stores a user's WhatsApp session.
type waSessionDoc struct {
	UserID      string    `bson:"user_id"`
	SessionData []byte    `bson:"session_data"` // SQLite file bytes (base64-unencoded binary)
	WAPhone     string    `bson:"wa_phone"`
	UpdatedAt   time.Time `bson:"updated_at"`
}

type WhatsAppService struct {
	client         *whatsmeow.Client
	container      *sqlstore.Container
	qrChan         chan string
	readyChan      chan bool
	disconnectChan chan string
	mu             sync.RWMutex
	isReady        bool
	lastQR         string

	// session persistence
	db      *db.DB
	userID  string
	dbPath  string // path to the temp SQLite file
	stopSync chan struct{}
}

func NewWhatsAppService() (*WhatsAppService, error) {
	dbPath := os.Getenv("WHATSAPP_SESSION_PATH")
	if dbPath == "" {
		dbPath = "./whatsapp_session.db"
	}
	return NewWhatsAppServiceWithPath(dbPath, "", nil)
}

func init() {
	// Set device name shown in WhatsApp's "Linked Devices" list
	store.SetOSInfo("BulkSend", [3]uint32{1, 0, 0})
}

func NewWhatsAppServiceWithPath(dbPath, userID string, database *db.DB) (*WhatsAppService, error) {
	svc := &WhatsAppService{
		qrChan:         make(chan string, 10),
		readyChan:      make(chan bool, 10),
		disconnectChan: make(chan string, 10),
		stopSync:       make(chan struct{}),
		db:             database,
		userID:         userID,
		dbPath:         dbPath,
	}

	// If MongoDB is available, try to restore the session from it.
	if database != nil && userID != "" {
		if err := svc.restoreFromMongo(context.Background()); err != nil {
			logger.Warn("Could not restore session from MongoDB for user %s: %v", userID, err)
		}
	}

	dbLog := waLog.Stdout("Database", "ERROR", true)
	container, err := sqlstore.New(context.Background(), "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), dbLog)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}
	svc.container = container
	return svc, nil
}

// restoreFromMongo loads the SQLite session bytes from MongoDB and writes them to dbPath.
func (s *WhatsAppService) restoreFromMongo(ctx context.Context) error {
	var doc waSessionDoc
	err := s.db.WASessions().FindOne(ctx, bson.M{"user_id": s.userID}).Decode(&doc)
	if err != nil {
		return nil // no session stored yet — not an error
	}
	if len(doc.SessionData) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.dbPath), 0755); err != nil {
		return err
	}
	logger.Info("Restoring WhatsApp session from MongoDB for user %s", s.userID)
	return os.WriteFile(s.dbPath, doc.SessionData, 0600)
}

// syncToMongo reads the current SQLite file and saves it to MongoDB.
func (s *WhatsAppService) syncToMongo(waPhone string) {
	if s.db == nil || s.userID == "" {
		return
	}

	// Checkpoint WAL so the main db file is up-to-date before we read it.
	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", s.dbPath))
	if err == nil {
		sqlDB.Exec("PRAGMA wal_checkpoint(FULL)") //nolint
		sqlDB.Close()
	}

	data, err := os.ReadFile(s.dbPath)
	if err != nil {
		logger.Warn("syncToMongo: could not read session file: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	doc := waSessionDoc{
		UserID:      s.userID,
		SessionData: data,
		WAPhone:     waPhone,
		UpdatedAt:   time.Now(),
	}
	opts := options.Replace().SetUpsert(true)
	_, err = s.db.WASessions().ReplaceOne(ctx, bson.M{"user_id": s.userID}, doc, opts)
	if err != nil {
		logger.Warn("syncToMongo: could not save session to MongoDB: %v", err)
		return
	}
	logger.Info("WhatsApp session synced to MongoDB for user %s", s.userID)
}

// startPeriodicSync syncs the session to MongoDB every 2 minutes while connected.
func (s *WhatsAppService) startPeriodicSync() {
	ticker := time.NewTicker(2 * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.mu.RLock()
				waPhone := ""
				if s.client != nil && s.client.Store.ID != nil {
					waPhone = s.client.Store.ID.String()
				}
				connected := s.client != nil && s.client.IsConnected()
				s.mu.RUnlock()
				if connected {
					s.syncToMongo(waPhone)
				}
			case <-s.stopSync:
				return
			}
		}
	}()
}

func (s *WhatsAppService) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil && s.client.IsConnected() && s.isReady {
		logger.Info("WhatsApp client already connected and ready")
		select {
		case s.readyChan <- true:
		default:
		}
		return nil
	}

	if s.client != nil {
		logger.Info("Cleaning up existing disconnected client...")
		s.client.Disconnect()
		s.client = nil
		s.isReady = false
		s.lastQR = ""
	}

	logger.Info("Initializing WhatsApp client...")

	deviceStore, err := s.container.GetFirstDevice(context.Background())
	if err != nil {
		logger.Error("Failed to get device from store: %v", err)
		return fmt.Errorf("failed to get device: %w", err)
	}

	hasSession := deviceStore.ID != nil
	if hasSession {
		logger.Info("Found existing session (Device ID: *****%s)",
			deviceStore.ID.String()[max(0, len(deviceStore.ID.String())-8):])
	} else {
		logger.Info("No existing session — will generate QR code for new login")
	}

	clientLog := waLog.Stdout("WhatsApp", "ERROR", true)
	s.client = whatsmeow.NewClient(deviceStore, clientLog)
	s.client.AddEventHandler(s.handleEvents)

	if hasSession {
		// Existing session — just connect, no QR needed
		logger.Info("Reconnecting with existing session...")
		if err := s.client.Connect(); err != nil {
			logger.Error("Failed to connect to WhatsApp: %v", err)
			// Connection failed — clear stale session and fall through to QR
			logger.Info("Clearing stale session and falling through to QR generation...")
			s.client.Disconnect()
			if deviceStore.ID != nil {
				_ = deviceStore.Delete(context.Background())
			}
			s.client = nil
			s.isReady = false
			s.lastQR = ""
			hasSession = false
		} else {
			// Wait for Connected event
			time.Sleep(3 * time.Second)
			if s.isReady {
				logger.Success("Reconnected with existing session!")
				select {
				case s.readyChan <- true:
				default:
				}
			} else {
				// Session exists but failed to become ready — stale session
				logger.Warn("Existing session could not reconnect — clearing and generating new QR")
				s.client.Disconnect()
				if deviceStore.ID != nil {
					_ = deviceStore.Delete(context.Background())
				}
				s.client = nil
				s.isReady = false
				s.lastQR = ""
				hasSession = false
			}
		}
	}

	if !hasSession {
		// No session (or stale session cleared) — get QR channel for new login
		logger.Info("Generating QR code for new login...")

		// Re-create device store if we cleared a stale one
		if s.client == nil {
			deviceStore, err = s.container.GetFirstDevice(context.Background())
			if err != nil {
				return fmt.Errorf("failed to get device: %w", err)
			}
			clientLog := waLog.Stdout("WhatsApp", "ERROR", true)
			s.client = whatsmeow.NewClient(deviceStore, clientLog)
			s.client.AddEventHandler(s.handleEvents)
		}

		qrChan, err := s.client.GetQRChannel(context.Background())
		if err != nil {
			logger.Error("Failed to get QR channel: %v", err)
			return fmt.Errorf("failed to get QR channel: %w", err)
		}

		go func() {
			for evt := range qrChan {
				if evt.Event == "code" {
					logger.Info("New QR code generated")
					qrCode, err := s.generateQRDataURL(evt.Code)
					if err == nil {
						s.mu.Lock()
						s.lastQR = qrCode
						s.mu.Unlock()
						select {
						case s.qrChan <- qrCode:
						default:
						}
					}
				}
			}
		}()

		if err := s.client.Connect(); err != nil {
			logger.Error("Failed to connect to WhatsApp: %v", err)
			return fmt.Errorf("failed to connect: %w", err)
		}
	}

	return nil
}

func (s *WhatsAppService) handleEvents(evt interface{}) {
	switch v := evt.(type) {
	case *events.LoggedOut:
		s.mu.Lock()
		s.isReady = false
		s.mu.Unlock()
		logger.Warn("WhatsApp logged out (reason: %v)", v.Reason)
		select {
		case s.disconnectChan <- "logged out":
		default:
		}

	case *events.Connected:
		if s.client.Store.ID != nil {
			s.mu.Lock()
			s.isReady = true
			waPhone := s.client.Store.ID.String()
			s.mu.Unlock()

			logger.Success("WhatsApp connected! Phone: %s", waPhone)

			select {
			case s.readyChan <- true:
			default:
			}

			// Persist session to MongoDB and start periodic sync
			go s.syncToMongo(waPhone)
			go s.startPeriodicSync()
		}

	case *events.Disconnected:
		s.mu.Lock()
		s.isReady = false
		s.mu.Unlock()
		logger.Warn("WhatsApp disconnected")
		select {
		case s.disconnectChan <- "disconnected":
		default:
		}

	case *events.StreamReplaced:
		s.mu.Lock()
		s.isReady = false
		s.mu.Unlock()
		select {
		case s.disconnectChan <- "stream replaced":
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

func (s *WhatsAppService) GetQRChannel() <-chan string  { return s.qrChan }
func (s *WhatsAppService) GetReadyChannel() <-chan bool { return s.readyChan }
func (s *WhatsAppService) GetDisconnectChannel() <-chan string { return s.disconnectChan }

func (s *WhatsAppService) GetStatus() (isConnected, isReady, hasQR bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	isConnected = s.client != nil && s.client.IsConnected()
	isReady = s.isReady && isConnected // isReady requires a live connection
	hasQR = s.lastQR != ""
	return
}

func (s *WhatsAppService) GetLastQR() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastQR
}

func (s *WhatsAppService) IsReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isReady && s.client != nil && s.client.IsConnected()
}

func (s *WhatsAppService) SendMessage(phone, message string) error {
	s.mu.RLock()
	client := s.client
	ready := s.isReady
	s.mu.RUnlock()

	// Guard: require both isReady flag AND a live WebSocket connection.
	if !ready || client == nil || !client.IsConnected() {
		logger.Error("WhatsApp not ready — isReady: %v, connected: %v",
			ready, client != nil && client.IsConnected())
		return fmt.Errorf("WhatsApp client not ready or disconnected")
	}

	jid, normalizedPhone, err := s.parseAndNormalizePhone(phone)
	if err != nil {
		logger.Error("Invalid phone number '%s': %v", phone, err)
		return fmt.Errorf("invalid phone number: %w", err)
	}

	logger.Info("Checking if %s is on WhatsApp...", normalizedPhone)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	isOnWhatsAppResult, err := client.IsOnWhatsApp(ctx, []string{normalizedPhone})
	if err != nil {
		logger.Warn("IsOnWhatsApp check failed for %s: %v (attempting send anyway)", normalizedPhone, err)
	} else if len(isOnWhatsAppResult) > 0 {
		if !isOnWhatsAppResult[0].IsIn {
			logger.Info("Number %s is NOT registered on WhatsApp", phone)
			return fmt.Errorf("number %s is not registered on WhatsApp", phone)
		}
		jid = isOnWhatsAppResult[0].JID
		logger.Info("Verified %s on WhatsApp → JID: %s", phone, jid.String())
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()

	resp, err := client.SendMessage(ctx2, jid, &waProto.Message{
		Conversation: proto.String(message),
	})
	if err != nil {
		logger.Error("SendMessage failed for %s: %v", phone, err)
		return fmt.Errorf("failed to send message: %w", err)
	}
	if resp.ID == "" {
		return fmt.Errorf("message sent but no confirmation received from server")
	}

	logger.Info("Message delivered — ID: %s, Timestamp: %v", resp.ID, resp.Timestamp)
	return nil
}

func (s *WhatsAppService) parseAndNormalizePhone(phone string) (types.JID, string, error) {
	cleanPhone := ""
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			cleanPhone += string(char)
		}
	}
	if cleanPhone == "" {
		return types.JID{}, "", fmt.Errorf("invalid phone number: no digits found")
	}
	if len(cleanPhone) < 10 {
		return types.JID{}, "", fmt.Errorf("phone number too short: %s", cleanPhone)
	}
	cleanPhone = strings.TrimLeft(cleanPhone, "0")
	normalizedPhone := "+" + cleanPhone
	jid := types.JID{
		User:   cleanPhone,
		Server: types.DefaultUserServer,
	}
	return jid, normalizedPhone, nil
}

func (s *WhatsAppService) Disconnect() {
	// Stop periodic sync
	select {
	case s.stopSync <- struct{}{}:
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	logger.Info("Disconnecting WhatsApp client...")

	if s.client != nil {
		waPhone := ""
		if s.client.Store.ID != nil {
			waPhone = s.client.Store.ID.String()
		}

		deviceStore := s.client.Store

		if err := s.client.Logout(context.Background()); err != nil {
			logger.Warn("Logout error (non-critical): %v", err)
		}
		s.client.Disconnect()

		if deviceStore != nil && deviceStore.ID != nil {
			if err := deviceStore.Delete(context.Background()); err != nil {
				logger.Warn("Failed to delete session from SQLite: %v", err)
			}
		}

		// Remove session from MongoDB on explicit disconnect/logout
		if s.db != nil && s.userID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s.db.WASessions().DeleteOne(ctx, bson.M{"user_id": s.userID}) //nolint
			logger.Info("WhatsApp session removed from MongoDB for user %s", s.userID)
		}

		_ = waPhone
		s.client = nil
	}

	s.isReady = false
	s.lastQR = ""
	logger.Success("WhatsApp disconnected")
}

func (s *WhatsAppService) SendWithDelay(phone, message string, delay time.Duration) error {
	time.Sleep(delay)
	return s.SendMessage(phone, message)
}

func (s *WhatsAppService) SendMessageWithImage(phone, message, imagePath string) error {
	s.mu.RLock()
	client := s.client
	ready := s.isReady
	s.mu.RUnlock()

	if !ready || client == nil || !client.IsConnected() {
		return fmt.Errorf("WhatsApp client not ready or disconnected")
	}

	jid, normalizedPhone, err := s.parseAndNormalizePhone(phone)
	if err != nil {
		return fmt.Errorf("invalid phone number: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	isOnWhatsAppResult, err := client.IsOnWhatsApp(ctx, []string{normalizedPhone})
	if err != nil {
		logger.Warn("IsOnWhatsApp check failed for %s: %v", normalizedPhone, err)
	} else if len(isOnWhatsAppResult) > 0 {
		if !isOnWhatsAppResult[0].IsIn {
			return fmt.Errorf("number %s is not registered on WhatsApp", phone)
		}
		jid = isOnWhatsAppResult[0].JID
	}

	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return fmt.Errorf("failed to read image: %w", err)
	}

	mimeType := detectImageMimeType(imageData, imagePath)
	logger.Info("Image loaded: %s (size: %d bytes, mime: %s)", imagePath, len(imageData), mimeType)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	uploaded, err := client.Upload(ctx2, imageData, whatsmeow.MediaImage)
	if err != nil {
		logger.Error("Failed to upload image to WhatsApp servers: %v", err)
		return fmt.Errorf("failed to upload image: %w", err)
	}
	logger.Info("Image uploaded to WhatsApp servers successfully (URL: %s)", uploaded.URL)

	imageMsg := &waProto.ImageMessage{
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		Mimetype:      proto.String(mimeType),
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(imageData))),
	}
	if message != "" {
		imageMsg.Caption = proto.String(message)
	}

	ctx3, cancel3 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel3()

	resp, err := client.SendMessage(ctx3, jid, &waProto.Message{ImageMessage: imageMsg})
	if err != nil {
		logger.Error("Failed to send image message to %s: %v", phone, err)
		return fmt.Errorf("failed to send image message: %w", err)
	}
	if resp.ID == "" {
		return fmt.Errorf("message sent but no confirmation received")
	}
	logger.Info("Image message delivered — ID: %s, Timestamp: %v", resp.ID, resp.Timestamp)
	return nil
}

func (s *WhatsAppService) SendMessageWithImageURL(phone, message, imageURL string) error {
	logger.Info("Downloading image from URL: %s", imageURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download image: HTTP %d", resp.StatusCode)
	}

	tempFile := filepath.Join(os.TempDir(),
		fmt.Sprintf("whatsapp_img_%d%s", time.Now().Unix(), filepath.Ext(imageURL)))
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile)

	if _, err = io.Copy(file, resp.Body); err != nil {
		file.Close()
		return fmt.Errorf("failed to save image: %w", err)
	}
	// Must close the file before reading it in SendMessageWithImage
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to flush temp image file: %w", err)
	}
	return s.SendMessageWithImage(phone, message, tempFile)
}

// detectImageMimeType detects the MIME type of image data using magic bytes.
// Go's http.DetectContentType does not recognize WebP, so we check manually.
func detectImageMimeType(data []byte, filePath string) string {
	if len(data) >= 12 {
		// WebP: starts with "RIFF" + 4 bytes size + "WEBP"
		if string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
			return "image/webp"
		}
	}
	// Fall back to Go's built-in detection
	mime := http.DetectContentType(data)
	// If detection failed, try to infer from file extension
	if mime == "application/octet-stream" {
		switch strings.ToLower(filepath.Ext(filePath)) {
		case ".webp":
			return "image/webp"
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".png":
			return "image/png"
		case ".gif":
			return "image/gif"
		}
	}
	return mime
}

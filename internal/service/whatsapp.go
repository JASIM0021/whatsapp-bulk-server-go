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

// waSessionDoc is the MongoDB document that stores a user's WhatsApp session identity.
// Only the JID (device ID) is stored here; the actual crypto session lives in the
// shared SQLite container managed by sqlstore.
type waSessionDoc struct {
	UserID    string    `bson:"user_id"`
	WAJID     string    `bson:"wa_jid"`   // full JID string, e.g. "919062955974:15@s.whatsapp.net"
	WAPhone   string    `bson:"wa_phone"` // human-readable phone number
	UpdatedAt time.Time `bson:"updated_at"`
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
	db                 *db.DB
	userID             string
	stopSync           chan struct{}
	hadRestoredSession bool // true when the user's JID was found in the shared container on startup

	// bot support
	botService interface {
		HandleIncomingMessage(ctx context.Context, userID, senderPhone, messageText string, waReply func(phone, text string) error)
		HandleSelfCommand(ctx context.Context, userID, text string, waReply func(phone, text string) error)
	}
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
	dbLog := waLog.Stdout("Database", "ERROR", true)
	container, err := sqlstore.New(context.Background(), "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), dbLog)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}
	return NewWhatsAppServiceWithContainer(container, userID, database)
}

// SharedContainer is a type alias for *sqlstore.Container.
// Exposed so callers (e.g. handlers) don't need to import whatsmeow/store/sqlstore directly.
type SharedContainer = *sqlstore.Container

// NewSharedContainer opens (or creates) a single SQLite database to hold all users'
// WhatsApp session data. Pass the returned container to NewWhatsAppServiceWithContainer.
// One container for all users = one file on disk regardless of user count.
func NewSharedContainer(dbPath string) (SharedContainer, error) {
	dbLog := waLog.Stdout("SharedWADB", "ERROR", true)
	return sqlstore.New(context.Background(), "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), dbLog)
}

// NewWhatsAppServiceWithContainer creates a WhatsApp service that shares a single
// sqlstore.Container across all users. Each user's session is identified by their
// WhatsApp JID stored in MongoDB — no per-user SQLite files are created.
func NewWhatsAppServiceWithContainer(container SharedContainer, userID string, database *db.DB) (*WhatsAppService, error) {
	svc := &WhatsAppService{
		qrChan:         make(chan string, 10),
		readyChan:      make(chan bool, 10),
		disconnectChan: make(chan string, 10),
		stopSync:       make(chan struct{}),
		container:      container,
		db:             database,
		userID:         userID,
	}
	// If MongoDB records a JID for this user, and that device exists in the shared
	// container, mark the session as restorable so the handler auto-reconnects.
	if database != nil && userID != "" {
		if svc.findDeviceByStoredJID() != nil {
			svc.hadRestoredSession = true
		}
	}
	return svc, nil
}

// loadJIDFromMongo returns the WhatsApp JID string stored for this user, or "" if none.
func (s *WhatsAppService) loadJIDFromMongo(ctx context.Context) string {
	if s.db == nil || s.userID == "" {
		return ""
	}
	var doc waSessionDoc
	if err := s.db.WASessions().FindOne(ctx, bson.M{"user_id": s.userID}).Decode(&doc); err != nil {
		return ""
	}
	return doc.WAJID
}

// findDeviceByStoredJID looks up the user's JID in MongoDB then finds the matching
// device in the shared sqlstore container. Returns nil if not found.
func (s *WhatsAppService) findDeviceByStoredJID() *store.Device {
	jid := s.loadJIDFromMongo(context.Background())
	if jid == "" {
		return nil
	}
	devices, err := s.container.GetAllDevices(context.Background())
	if err != nil {
		return nil
	}
	for _, d := range devices {
		if d.ID != nil && d.ID.String() == jid {
			return d
		}
	}
	return nil
}

// saveJIDToMongo persists the user's WhatsApp JID to MongoDB.
// Called once after a successful QR scan; the JID never changes after that.
func (s *WhatsAppService) saveJIDToMongo(waJID, waPhone string) {
	if s.db == nil || s.userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc := waSessionDoc{
		UserID:    s.userID,
		WAJID:     waJID,
		WAPhone:   waPhone,
		UpdatedAt: time.Now(),
	}
	_, err := s.db.WASessions().ReplaceOne(ctx, bson.M{"user_id": s.userID}, doc,
		options.Replace().SetUpsert(true))
	if err != nil {
		logger.Warn("saveJIDToMongo: %v", err)
		return
	}
	logger.Info("WhatsApp JID saved to MongoDB for user %s (%s)", s.userID, waPhone)
}

// clearJIDFromMongo removes the stored JID for this user (called when a session goes stale).
func (s *WhatsAppService) clearJIDFromMongo() {
	if s.db == nil || s.userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = s.db.WASessions().DeleteOne(ctx, bson.M{"user_id": s.userID})
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

	// Look up the user's device in the shared container by their stored JID.
	// Returns nil for first-time logins (no JID stored yet).
	deviceStore := s.findDeviceByStoredJID()
	hasSession := deviceStore != nil

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
		// Existing session — try to reconnect
		logger.Info("Reconnecting with existing session...")
		if err := s.client.Connect(); err != nil {
			logger.Error("Failed to connect with existing session: %v", err)
			// Session is stale — remove from container and MongoDB, then re-QR
			logger.Info("Deleting stale session and generating new QR code...")
			s.client.Disconnect()
			_ = deviceStore.Delete(context.Background())
			go s.clearJIDFromMongo()
			s.client = nil
			s.isReady = false
			return s.initWithQRCodeLocked()
		}

		// Wait up to 5 seconds for the connection to become ready
		s.mu.Unlock()
		readyTimeout := time.After(5 * time.Second)
		connected := false
		select {
		case <-s.readyChan:
			connected = true
		case <-readyTimeout:
		}
		s.mu.Lock()

		if connected && s.isReady {
			logger.Success("Reconnected with existing session!")
			select {
			case s.readyChan <- true:
			default:
			}
		} else {
			// Session could not reconnect — clear and re-QR
			logger.Warn("Existing session could not reconnect — clearing and generating QR code")
			s.client.Disconnect()
			_ = deviceStore.Delete(context.Background())
			go s.clearJIDFromMongo()
			s.client = nil
			s.isReady = false
			s.lastQR = ""
			return s.initWithQRCodeLocked()
		}
	} else {
		return s.initWithQRCodeLocked()
	}

	return nil
}

// initWithQRCodeLocked creates a fresh client and starts QR code generation.
// Must be called with s.mu held.
func (s *WhatsAppService) initWithQRCodeLocked() error {
	// NewDevice creates an empty in-memory device backed by the shared container.
	// It gets a real JID (and is saved to the container's SQLite) only after the
	// user scans the QR code and WhatsApp assigns a device ID.
	deviceStore := s.container.NewDevice()

	clientLog := waLog.Stdout("WhatsApp", "ERROR", true)
	s.client = whatsmeow.NewClient(deviceStore, clientLog)
	s.client.AddEventHandler(s.handleEvents)

	logger.Info("Generating QR code for new login")
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

	return nil
}

func (s *WhatsAppService) handleEvents(evt interface{}) {
	switch v := evt.(type) {
	case *events.LoggedOut:
		s.mu.Lock()
		s.isReady = false
		s.lastQR = ""
		client := s.client
		s.mu.Unlock()
		logger.Warn("WhatsApp logged out (reason: %v)", v.Reason)
		select {
		case s.disconnectChan <- "logged out":
		default:
		}

		// Auto-reinitialize: delete stale session and generate new QR code
		go func() {
			time.Sleep(1 * time.Second)
			s.mu.Lock()
			if client != nil {
				client.Disconnect()
			}
			if client != nil && client.Store != nil && client.Store.ID != nil {
				_ = client.Store.Delete(context.Background())
			}
			s.client = nil
			s.mu.Unlock()

			logger.Info("Auto-reinitializing after logout — generating new QR code")
			if err := s.Initialize(); err != nil {
				logger.Error("Auto-reinitialize failed after logout: %v", err)
			}
		}()

	case *events.Connected:
		if s.client.Store.ID != nil {
			s.mu.Lock()
			s.isReady = true
			waJID := s.client.Store.ID.String()
			waPhone := "+" + s.client.Store.ID.User
			s.mu.Unlock()

			logger.Success("WhatsApp connected! Phone: %s", waPhone)

			select {
			case s.readyChan <- true:
			default:
			}

			// Persist the JID to MongoDB so we can reconnect after server restart.
			// The actual session crypto is already in the shared SQLite container.
			go s.saveJIDToMongo(waJID, waPhone)
		}

	case *events.Disconnected:
		s.mu.Lock()
		wasReady := s.isReady
		s.isReady = false
		s.mu.Unlock()
		logger.Warn("WhatsApp disconnected")
		select {
		case s.disconnectChan <- "disconnected":
		default:
		}

		// If we were previously connected, try to auto-reconnect
		if wasReady {
			go func() {
				time.Sleep(3 * time.Second)
				logger.Info("Attempting auto-reconnect after disconnect...")
				if err := s.Initialize(); err != nil {
					logger.Error("Auto-reconnect failed: %v", err)
				}
			}()
		}

	case *events.StreamReplaced:
		s.mu.Lock()
		s.isReady = false
		s.mu.Unlock()
		logger.Warn("WhatsApp stream replaced")
		select {
		case s.disconnectChan <- "stream replaced":
		default:
		}

	case *events.TemporaryBan:
		logger.Warn("WhatsApp temporary ban (code: %v)", v.Code)

	case *events.Message:
		// Drop groups and broadcasts immediately.
		if v.Info.IsGroup || v.Info.Chat.Server == "broadcast" {
			return
		}

		s.mu.RLock()
		botSvc := s.botService
		userID := s.userID
		s.mu.RUnlock()

		// Detect self-chat: Chat.User matches the device's own phone number.
		// This works whether WhatsApp delivers the echo as IsFromMe=true or false.
		ownPhone := ""
		if s.client != nil && s.client.Store.ID != nil {
			ownPhone = s.client.Store.ID.User
		}
		if ownPhone != "" && v.Info.Chat.User == ownPhone {
			text := strings.TrimSpace(extractMessageText(v.Message))
			if text != "" && botSvc != nil && userID != "" {
				go botSvc.HandleSelfCommand(context.Background(), userID, text,
					func(_, reply string) error {
						return s.SendMessage(ownPhone, reply)
					},
				)
			}
			return
		}

		// Skip all other outgoing messages (sent to someone else).
		if v.Info.IsFromMe {
			return
		}
		text := strings.TrimSpace(extractMessageText(v.Message))
		if text == "" {
			return // media, sticker, reaction, etc. — nothing to reply to
		}
		// WhatsApp uses two addressing modes:
		//   - PN (phone number): Sender.User contains the real phone number
		//   - LID (Long-term ID): Sender.User contains an internal ID, NOT a phone.
		//     The real phone is in SenderAlt.User when available.
		// Skip LID addresses with no phone fallback to avoid processing bot/automated messages.
		if v.Info.AddressingMode == types.AddressingModeLID && v.Info.SenderAlt.User == "" {
			return
		}
		senderUser := v.Info.Sender.User
		if v.Info.AddressingMode == types.AddressingModeLID && v.Info.SenderAlt.User != "" {
			senderUser = v.Info.SenderAlt.User
		}
		if senderUser == "" {
			return
		}
		// senderPhone := "+" + senderUser
		senderPhone, ok := ExtractSenderPhone(&v.Info)
		if !ok {
			return
		}
		logger.Info("Bot: incoming message from %s → %q", senderPhone, text)

		if botSvc == nil {
			logger.Info("Bot: no bot service injected for user %s — skipping", userID)
			return
		}
		if userID == "" {
			logger.Info("Bot: WhatsApp service has no userID — skipping")
			return
		}
		go botSvc.HandleIncomingMessage(
			context.Background(),
			userID,
			senderPhone,
			text,
			func(phone, reply string) error {
				return s.SendMessage(phone, reply)
			},
		)
	}
}

func ExtractSenderPhone(info *types.MessageInfo) (string, bool) {
	if info == nil {
		return "", false
	}

	// Skip invalid/system messages
	if info.IsFromMe || info.Chat.Server == "broadcast" {
		return "", false
	}

	var candidates []string

	// 1. Highest priority: SenderAlt (real phone in LID mode)
	if info.SenderAlt.User != "" {
		candidates = append(candidates, info.SenderAlt.User)
	}

	// 2. Chat user (VERY important for 1:1 LID messages)
	if info.Chat.User != "" {
		candidates = append(candidates, info.Chat.User)
	}

	// 3. Sender (PN mode OR fallback)
	if info.Sender.User != "" {
		candidates = append(candidates, info.Sender.User)
	}

	// Try to extract a valid phone from candidates
	for _, user := range candidates {
		phone := normalizePhone(user)
		if isValidPhone(phone) {
			return phone, true
		}
	}

	return "", false
}

func normalizePhone(user string) string {
	// Remove domain if present
	user = strings.Split(user, "@")[0]

	// Keep only digits
	clean := ""
	for _, ch := range user {
		if ch >= '0' && ch <= '9' {
			clean += string(ch)
		}
	}

	if clean == "" {
		return ""
	}

	return "+" + clean
}

func isValidPhone(phone string) bool {
	// Basic validation: must be at least 10 digits
	if len(phone) < 11 { // + + 10 digits
		return false
	}

	// Reject obvious LID/internal IDs (too long or weird)
	if len(phone) > 16 {
		return false
	}

	return true
}

// extractMessageText pulls plain text out of any common whatsmeow message wrapper.
// Returns "" for media, stickers, reactions, and other non-text content.
func extractMessageText(msg *waProto.Message) string {
	if msg == nil {
		return ""
	}
	// Direct text
	if msg.Conversation != nil && *msg.Conversation != "" {
		return *msg.Conversation
	}
	// Text with link preview / formatting
	if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Text != nil {
		return *msg.ExtendedTextMessage.Text
	}
	// Disappearing messages
	if msg.EphemeralMessage != nil {
		return extractMessageText(msg.EphemeralMessage.Message)
	}
	// View-once messages
	if msg.ViewOnceMessage != nil {
		return extractMessageText(msg.ViewOnceMessage.Message)
	}
	// Messages sent from another device of the same account (forwarded to us by the server)
	if msg.DeviceSentMessage != nil {
		return extractMessageText(msg.DeviceSentMessage.Message)
	}
	// Button reply
	if msg.ButtonsResponseMessage != nil {
		if t := msg.ButtonsResponseMessage.GetSelectedDisplayText(); t != "" {
			return t
		}
	}
	// List reply
	if msg.ListResponseMessage != nil {
		if t := msg.ListResponseMessage.GetTitle(); t != "" {
			return t
		}
	}
	return ""
}

func (s *WhatsAppService) generateQRDataURL(code string) (string, error) {
	png, err := qrcode.Encode(code, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(png)
	return fmt.Sprintf("data:image/png;base64,%s", encoded), nil
}

// Reinitialize forces a fresh QR code generation by clearing any existing session.
func (s *WhatsAppService) Reinitialize() error {
	s.mu.Lock()
	if s.client != nil {
		s.client.Disconnect()
		if s.client.Store != nil && s.client.Store.ID != nil {
			_ = s.client.Store.Delete(context.Background())
		}
		s.client = nil
	}
	s.isReady = false
	s.lastQR = ""
	s.mu.Unlock()

	return s.Initialize()
}

// SetBotService injects the bot service so incoming WhatsApp messages can trigger auto-replies.
func (s *WhatsAppService) SetBotService(bs interface {
	HandleIncomingMessage(ctx context.Context, userID, senderPhone, messageText string, waReply func(phone, text string) error)
	HandleSelfCommand(ctx context.Context, userID, text string, waReply func(phone, text string) error)
}) {
	s.mu.Lock()
	s.botService = bs
	s.mu.Unlock()
}

// HasRestoredSession returns true when the session was loaded from MongoDB on startup.
func (s *WhatsAppService) HasRestoredSession() bool { return s.hadRestoredSession }

// GetUserID returns the immutable user ID this service belongs to.
// Used by callers to assert session ownership before sending.
func (s *WhatsAppService) GetUserID() string { return s.userID }

func (s *WhatsAppService) GetQRChannel() <-chan string         { return s.qrChan }
func (s *WhatsAppService) GetReadyChannel() <-chan bool        { return s.readyChan }
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

// GracefulShutdown closes the WebSocket connection without logging out.
// Sessions remain valid in SQLite so the client can reconnect on next startup
// without requiring a new QR scan. Use this for server restarts / PM2 reloads.
func (s *WhatsAppService) GracefulShutdown() {
	// Stop periodic sync
	select {
	case s.stopSync <- struct{}{}:
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil {
		logger.Info("Graceful shutdown: closing WebSocket for user %s (session preserved)", s.userID)
		s.client.Disconnect() // close WS only — no Logout(), no SQLite/MongoDB delete
		s.isReady = false
		s.lastQR = ""
	}
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

// WAContact represents a contact fetched from the WhatsApp session store.
type WAContact struct {
	Phone string `json:"phone"`
	Name  string `json:"name"`
}

// GetWhatsAppContacts returns all contacts stored in the active WhatsApp session.
// Only phone contacts (JID server = s.whatsapp.net) are included.
func (s *WhatsAppService) GetWhatsAppContacts(ctx context.Context) ([]WAContact, error) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil || !s.IsReady() {
		return nil, fmt.Errorf("WhatsApp is not connected")
	}

	allContacts, err := client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch contacts: %w", err)
	}

	result := make([]WAContact, 0, len(allContacts))
	for jid, info := range allContacts {
		if jid.Server != "s.whatsapp.net" {
			continue // skip groups, broadcast lists, etc.
		}
		name := info.FullName
		if name == "" {
			name = info.PushName
		}
		if name == "" {
			name = info.FirstName
		}
		result = append(result, WAContact{
			Phone: "+" + jid.User,
			Name:  name,
		})
	}
	return result, nil
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

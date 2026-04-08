package types

// Contact represents a contact with phone number and name
type Contact struct {
	Phone string `json:"phone"`
	Name  string `json:"name"`
}

// Message represents a message to be sent
type Message struct {
	Text      string `json:"text"`
	Link      string `json:"link,omitempty"`
	ImageURL  string `json:"imageUrl,omitempty"`
	ImagePath string `json:"imagePath,omitempty"` // Local file path for uploaded images
}

// SendRequest represents the request body for sending messages
type SendRequest struct {
	Contacts []Contact `json:"contacts"`
	Message  Message   `json:"message"`
}

// UploadResponse represents the response for file upload
type UploadResponse struct {
	Contacts        []Contact `json:"contacts"`
	TotalContacts   int       `json:"totalContacts"`
	ValidContacts   int       `json:"validContacts"`
	InvalidContacts int       `json:"invalidContacts"`
}

// ProgressUpdate represents a progress update during message sending
type ProgressUpdate struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// SendProgress represents the progress of message sending
type SendProgress struct {
	Total   int      `json:"total"`
	Sent    int      `json:"sent"`
	Failed  int      `json:"failed"`
	Current *Contact `json:"current,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

// APIResponse represents a standard API response
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// WhatsAppStatus represents the WhatsApp connection status
type WhatsAppStatus struct {
	IsConnected   bool `json:"isConnected"`
	IsReady       bool `json:"isReady"`
	HasQR         bool `json:"hasQR"`
	IsInitialized bool `json:"isInitialized"`
}

// ImageUploadResponse represents the response for image upload
type ImageUploadResponse struct {
	Success  bool   `json:"success"`
	FilePath string `json:"filePath"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	MimeType string `json:"mimeType"`
	Error    string `json:"error,omitempty"`
}

// RegisterRequest is the request body for user registration
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// LoginRequest is the request body for user login
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AuthResponse is returned after successful login/register
type AuthResponse struct {
	Token string   `json:"token"`
	User  UserInfo `json:"user"`
}

// UserInfo represents public user fields
type UserInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// Template represents a message template
type Template struct {
	ID        string   `json:"id"`
	UserID    string   `json:"userId"`
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Body      string   `json:"body"`
	Variables []string `json:"variables"`
	IsDefault bool     `json:"isDefault"`
	CreatedAt string   `json:"createdAt"`
}

// CreateTemplateRequest is the request body for creating a template
type CreateTemplateRequest struct {
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Body      string   `json:"body"`
	Variables []string `json:"variables"`
}

// UpdateTemplateRequest is the request body for updating a template
type UpdateTemplateRequest struct {
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Body      string   `json:"body"`
	Variables []string `json:"variables"`
}

// contextKey is the type for context keys to avoid collisions
type contextKey string

// UserIDKey is the context key for the authenticated user's ID (string ObjectID hex)
const UserIDKey contextKey = "userID"

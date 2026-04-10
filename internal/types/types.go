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
	ID           string            `json:"id"`
	Email        string            `json:"email"`
	Name         string            `json:"name"`
	Role         string            `json:"role"`
	Subscription *SubscriptionInfo `json:"subscription,omitempty"`
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

// Subscription plan constants
const (
	PlanFree    = "free"
	PlanMonthly = "monthly"
	PlanYearly  = "yearly"
)

// Subscription represents a user's current subscription state
type Subscription struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Plan      string `json:"plan"`
	Status    string `json:"status"`
	StartDate string `json:"startDate"`
	ExpiryDate string `json:"expiryDate"`
	CreatedAt string `json:"createdAt"`
}

// SubscriptionInfo is the public subscription data returned by APIs
type SubscriptionInfo struct {
	Plan       string `json:"plan"`
	Status     string `json:"status"`
	ExpiryDate string `json:"expiryDate"`
	IsActive   bool   `json:"isActive"`
	DaysLeft   int    `json:"daysLeft"`
}

// Payment represents a single payment record
type Payment struct {
	ID           string  `json:"id"`
	UserID       string  `json:"userId"`
	TxnID        string  `json:"txnId"`
	Amount       float64 `json:"amount"`
	Plan         string  `json:"plan"`
	Status       string  `json:"status"`
	MihpayID     string  `json:"mihpayId,omitempty"`
	CreatedAt    string  `json:"createdAt"`
}

// InitiatePaymentRequest is the frontend request to start a payment
type InitiatePaymentRequest struct {
	Plan string `json:"plan"`
}

// PayUFormData is returned to frontend to build the hidden form POST to PayU
type PayUFormData struct {
	Action      string `json:"action"`
	Key         string `json:"key"`
	TxnID       string `json:"txnid"`
	Amount      string `json:"amount"`
	ProductInfo string `json:"productinfo"`
	FirstName   string `json:"firstname"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	Surl        string `json:"surl"`
	Furl        string `json:"furl"`
	Hash        string `json:"hash"`
	Udf1        string `json:"udf1"`
	Udf2        string `json:"udf2"`
	Udf3        string `json:"udf3"`
	Udf4        string `json:"udf4"`
	Udf5        string `json:"udf5"`
}

// contextKey is the type for context keys to avoid collisions
type contextKey string

// UserIDKey is the context key for the authenticated user's ID (string ObjectID hex)
const UserIDKey contextKey = "userID"

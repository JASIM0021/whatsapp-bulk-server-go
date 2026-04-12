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
	Plan         string `json:"plan"`
	Status       string `json:"status"`
	ExpiryDate   string `json:"expiryDate"`
	IsActive     bool   `json:"isActive"`
	DaysLeft     int    `json:"daysLeft"`
	MessagesUsed int    `json:"messagesUsed"`
	MessageLimit int    `json:"messageLimit"`
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
	Plan      string `json:"plan"`
	PromoCode string `json:"promoCode,omitempty"`
}

// PromoCode represents a discount code record returned by the API
type PromoCode struct {
	ID              string   `json:"id"`
	Code            string   `json:"code"`
	DiscountType    string   `json:"discountType"`    // "percentage" | "fixed"
	DiscountValue   float64  `json:"discountValue"`
	MaxUses         int      `json:"maxUses"`
	TimesUsed       int      `json:"timesUsed"`
	IsActive        bool     `json:"isActive"`
	ApplicablePlans []string `json:"applicablePlans"` // empty = all plans
	ExpiresAt       string   `json:"expiresAt,omitempty"`
	CreatedAt       string   `json:"createdAt"`
}

// CreatePromoCodeRequest is the request body for creating a promo code
type CreatePromoCodeRequest struct {
	Code            string   `json:"code"`
	DiscountType    string   `json:"discountType"`
	DiscountValue   float64  `json:"discountValue"`
	MaxUses         int      `json:"maxUses"`
	ApplicablePlans []string `json:"applicablePlans"`
	ExpiresAt       string   `json:"expiresAt,omitempty"` // ISO date string, optional
}

// UpdatePromoCodeRequest is the request body for updating a promo code
type UpdatePromoCodeRequest struct {
	IsActive      *bool    `json:"isActive,omitempty"`
	MaxUses       *int     `json:"maxUses,omitempty"`
	DiscountValue *float64 `json:"discountValue,omitempty"`
}

// ValidatePromoResponse is the response for promo code validation
type ValidatePromoResponse struct {
	Valid          bool    `json:"valid"`
	Code           string  `json:"code"`
	DiscountType   string  `json:"discountType"`
	DiscountValue  float64 `json:"discountValue"`
	OriginalAmount float64 `json:"originalAmount"`
	FinalAmount    float64 `json:"finalAmount"`
	DiscountAmount float64 `json:"discountAmount"`
	Message        string  `json:"message,omitempty"` // error reason if invalid
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

// SavedContact represents a contact stored in the user's address book
type SavedContact struct {
	ID        string `bson:"_id,omitempty" json:"id"`
	UserID    string `bson:"user_id"        json:"userId"`
	Name      string `bson:"name"           json:"name"`
	Phone     string `bson:"phone"          json:"phone"`
	CreatedAt string `bson:"created_at"     json:"createdAt"`
}

// SaveContactsRequest is the request body for POST /api/contacts
type SaveContactsRequest struct {
	Contacts []struct {
		Name  string `json:"name"`
		Phone string `json:"phone"`
	} `json:"contacts"`
}

// ForgotPasswordRequest is the request body for POST /api/auth/forgot-password
type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

// ResetPasswordRequest is the request body for POST /api/auth/reset-password
type ResetPasswordRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"newPassword"`
}

// SendOTPRequest is the request body for POST /api/auth/send-otp
type SendOTPRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// VerifyOTPRequest is the request body for POST /api/auth/verify-otp
type VerifyOTPRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// Invoice represents an invoice record created on payment success.
type Invoice struct {
	ID             string  `json:"id"`
	InvoiceNumber  string  `json:"invoiceNumber"`
	UserID         string  `json:"userId"`
	UserName       string  `json:"userName"`
	UserEmail      string  `json:"userEmail"`
	Plan           string  `json:"plan"`
	OriginalAmount float64 `json:"originalAmount"`
	FinalAmount    float64 `json:"finalAmount"`
	TxnID          string  `json:"txnId"`
	MihpayID       string  `json:"mihpayId"`
	PaymentDate    string  `json:"paymentDate"`
	ExpiryDate     string  `json:"expiryDate"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"createdAt"`
	SentAt         string  `json:"sentAt,omitempty"`
}

// PlanConfig represents the pricing configuration for a subscription plan.
type PlanConfig struct {
	Plan         string  `json:"plan"`
	Amount       float64 `json:"amount"`
	MessageLimit int     `json:"messageLimit"`
	UpdatedAt    string  `json:"updatedAt"`
}

// UpdatePlanConfigRequest is the request body for updating plan pricing.
type UpdatePlanConfigRequest struct {
	Amount       float64 `json:"amount"`
	MessageLimit int     `json:"messageLimit,omitempty"`
}

// UpdateInvoiceRequest is the request body for updating an invoice amount.
type UpdateInvoiceRequest struct {
	FinalAmount float64 `json:"finalAmount"`
}

// ApproveInvoiceRequest is the request body for approving and sending an invoice.
type ApproveInvoiceRequest struct {
	Amount float64 `json:"amount,omitempty"`
}

// contextKey is the type for context keys to avoid collisions
type contextKey string

// UserIDKey is the context key for the authenticated user's ID (string ObjectID hex)
const UserIDKey contextKey = "userID"

// APIKey represents a developer API key (returned to the frontend)
type APIKey struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Key        string `json:"key,omitempty"`  // only shown on creation
	KeyPreview string `json:"keyPreview"`     // e.g. "bsk_a1b2...ef12"
	IsActive   bool   `json:"isActive"`
	LastUsedAt string `json:"lastUsedAt,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

// CreateAPIKeyRequest is the request body for generating a new API key
type CreateAPIKeyRequest struct {
	Name string `json:"name"` // optional label
}

// ExternalSendRequest is the request body for the developer API POST /api/v1/send
type ExternalSendRequest struct {
	Phone    string `json:"phone,omitempty"`
	Contacts []struct {
		Phone string `json:"phone"`
		Name  string `json:"name,omitempty"`
	} `json:"contacts,omitempty"`
	Message struct {
		Text     string `json:"text"`
		ImageURL string `json:"imageUrl,omitempty"`
	} `json:"message"`
}

// ExternalSendResponse is the JSON response from POST /api/v1/send
type ExternalSendResponse struct {
	Success bool     `json:"success"`
	Sent    int      `json:"sent"`
	Failed  int      `json:"failed"`
	Total   int      `json:"total"`
	Errors  []string `json:"errors,omitempty"`
}

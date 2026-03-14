# WhatsApp Bulk Messaging Backend (Go + whatsmeow)

A high-performance Go backend for bulk WhatsApp messaging using the whatsmeow library.

## Features

- 🚀 **Fast & Efficient**: Built with Go for optimal performance
- 📱 **QR Code Authentication**: Easy WhatsApp Web login
- 📊 **File Upload**: Support for Excel (.xlsx, .xls) and CSV files
- 🔄 **Real-time Progress**: Server-Sent Events (SSE) for live updates
- ⏱️ **Rate Limiting**: Prevents WhatsApp bans with configurable delays
- 💾 **Session Persistence**: Maintains login across restarts
- 🔒 **CORS Support**: Secure cross-origin requests

## Prerequisites

- Go 1.21 or higher
- SQLite3

## Installation

1. **Install dependencies**:
```bash
cd backend-go
go mod download
```

2. **Configure environment**:
```bash
cp .env.example .env
# Edit .env if needed
```

3. **Build and run**:
```bash
go run cmd/server/main.go
```

Or build a binary:
```bash
go build -o server cmd/server/main.go
./server
```

## API Endpoints

### Health Check
```
GET /api/health
```

### WhatsApp Operations

#### Initialize WhatsApp Client
```
POST /api/whatsapp/init
```

#### Get QR Code (SSE Stream)
```
GET /api/whatsapp/qr
```
Returns a Server-Sent Events stream with QR codes and authentication status.

#### Get Status
```
GET /api/whatsapp/status
```

#### Disconnect
```
POST /api/whatsapp/disconnect
```

#### Send Bulk Messages (SSE Stream)
```
POST /api/whatsapp/send
Content-Type: application/json

{
  "contacts": [
    {"phone": "919876543210", "name": "John"},
    {"phone": "919876543211", "name": "Jane"}
  ],
  "message": {
    "text": "Hello {{name}}!",
    "link": "https://example.com"
  }
}
```
Returns a Server-Sent Events stream with progress updates.

### File Upload

#### Upload Excel/CSV File
```
POST /api/upload
Content-Type: multipart/form-data

file: [Excel or CSV file]
```

## Environment Variables

```env
PORT=4000                                    # Server port
NODE_ENV=development                         # Environment
FRONTEND_URL=http://localhost:5174           # Frontend URL
WHATSAPP_SESSION_PATH=./whatsapp_session.db  # Session database path
UPLOAD_DIR=./uploads                         # Upload directory
MAX_FILE_SIZE=10485760                       # Max file size (10MB)
RATE_LIMIT_MIN_DELAY=3000                    # Min delay between messages (ms)
RATE_LIMIT_MAX_DELAY=5000                    # Max delay between messages (ms)
```

## Project Structure

```
backend-go/
├── cmd/
│   └── server/
│       └── main.go              # Application entry point
├── internal/
│   ├── handler/                 # HTTP handlers
│   │   ├── whatsapp.go         # WhatsApp endpoints
│   │   └── upload.go           # File upload endpoint
│   ├── service/                # Business logic
│   │   └── whatsapp.go         # WhatsApp service (whatsmeow)
│   ├── middleware/             # HTTP middleware
│   │   └── cors.go            # CORS middleware
│   ├── types/                  # Type definitions
│   │   └── types.go
│   └── utils/                  # Utility functions
│       ├── parser.go          # Excel/CSV parsing
│       └── validation.go      # Phone validation
├── .env                        # Environment variables
├── .gitignore
├── go.mod                      # Go modules
├── go.sum
└── README.md
```

## Development

Run in development mode with auto-reload:
```bash
# Install air for hot reload
go install github.com/air-verse/air@latest

# Run with air
air
```

## Production Deployment

1. Build optimized binary:
```bash
CGO_ENABLED=1 go build -ldflags="-s -w" -o server cmd/server/main.go
```

2. Run:
```bash
./server
```

## Testing

```bash
# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests with race detection
go test -race ./...
```

## Troubleshooting

### WhatsApp Connection Issues

- Ensure you scan the QR code within 2 minutes
- Check that no other WhatsApp Web session is active
- Verify the session database is writable

### File Upload Issues

- Ensure uploads directory exists and is writable
- Check file size is under MAX_FILE_SIZE
- Verify file format is Excel (.xlsx, .xls) or CSV

### Rate Limiting

- Adjust RATE_LIMIT_MIN_DELAY and RATE_LIMIT_MAX_DELAY
- Default is 3-5 seconds between messages
- Recommended: Don't send more than 50 messages/hour

## License

MIT

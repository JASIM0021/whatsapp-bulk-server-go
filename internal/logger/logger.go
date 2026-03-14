package logger

import (
	"fmt"
	"log"
	"os"
	"time"
)

// Color codes for terminal output
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorWhite   = "\033[37m"
	ColorGray    = "\033[90m"
	ColorBold    = "\033[1m"
)

var (
	InfoLogger  *log.Logger
	ErrorLogger *log.Logger
	WarnLogger  *log.Logger
	DebugLogger *log.Logger
)

func init() {
	InfoLogger = log.New(os.Stdout, "", 0)
	ErrorLogger = log.New(os.Stderr, "", 0)
	WarnLogger = log.New(os.Stdout, "", 0)
	DebugLogger = log.New(os.Stdout, "", 0)
}

func timestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// Info logs an info message with green color
func Info(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	InfoLogger.Printf("%s[%s] %s✓%s %s%s", ColorGray, timestamp(), ColorGreen, ColorReset, msg, ColorReset)
}

// Error logs an error message with red color
func Error(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	ErrorLogger.Printf("%s[%s] %s✗%s %s%s", ColorGray, timestamp(), ColorRed, ColorReset, msg, ColorReset)
}

// Warn logs a warning message with yellow color
func Warn(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	WarnLogger.Printf("%s[%s] %s⚠%s  %s%s", ColorGray, timestamp(), ColorYellow, ColorReset, msg, ColorReset)
}

// Debug logs a debug message with cyan color
func Debug(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	DebugLogger.Printf("%s[%s] %s🔍%s %s%s", ColorGray, timestamp(), ColorCyan, ColorReset, msg, ColorReset)
}

// Success logs a success message with bold green color
func Success(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	InfoLogger.Printf("%s[%s] %s%s✅ %s%s%s", ColorGray, timestamp(), ColorGreen, ColorBold, msg, ColorReset, ColorReset)
}

// HTTP logs an HTTP request with method and path
func HTTP(method, path string, statusCode int, duration time.Duration) {
	var color string
	switch {
	case statusCode >= 500:
		color = ColorRed
	case statusCode >= 400:
		color = ColorYellow
	case statusCode >= 300:
		color = ColorCyan
	case statusCode >= 200:
		color = ColorGreen
	default:
		color = ColorWhite
	}

	InfoLogger.Printf("%s[%s] %s%s %s%s %s%d%s %s(%s)%s",
		ColorGray, timestamp(),
		ColorBlue, method, path, ColorReset,
		color, statusCode, ColorReset,
		ColorGray, duration, ColorReset)
}

// Section logs a section header
func Section(title string) {
	border := "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	InfoLogger.Printf("%s%s%s", ColorCyan, border, ColorReset)
	InfoLogger.Printf("%s%s %s %s%s", ColorCyan, ColorBold, title, ColorReset, ColorReset)
	InfoLogger.Printf("%s%s%s", ColorCyan, border, ColorReset)
}

// WhatsApp specific loggers
func WhatsAppSending(index, total int, name, phone string) {
	InfoLogger.Printf("%s[%s] %s📤 [%d/%d]%s Sending to: %s%s%s (%s)",
		ColorGray, timestamp(),
		ColorBlue, index, total, ColorReset,
		ColorBold, name, ColorReset, phone)
}

func WhatsAppSuccess(index, total int, name, phone, msgID string) {
	InfoLogger.Printf("%s[%s] %s✅ [%d/%d]%s Successfully sent to: %s%s%s (%s) - Message ID: %s%s%s",
		ColorGray, timestamp(),
		ColorGreen, index, total, ColorReset,
		ColorBold, name, ColorReset, phone,
		ColorGray, msgID, ColorReset)
}

func WhatsAppFailed(index, total int, name, phone string, err error) {
	ErrorLogger.Printf("%s[%s] %s❌ [%d/%d]%s Failed to send to: %s%s%s (%s) - Error: %s%s%s",
		ColorGray, timestamp(),
		ColorRed, index, total, ColorReset,
		ColorBold, name, ColorReset, phone,
		ColorRed, err.Error(), ColorReset)
}

func WhatsAppDelay(duration time.Duration) {
	InfoLogger.Printf("%s[%s] %s⏳%s Waiting %s before next message...",
		ColorGray, timestamp(),
		ColorYellow, ColorReset, duration)
}

func WhatsAppVerified(phone, jid string) {
	InfoLogger.Printf("%s[%s] %s✓%s Number verified on WhatsApp: %s (JID: %s%s%s)",
		ColorGray, timestamp(),
		ColorGreen, ColorReset, phone,
		ColorGray, jid, ColorReset)
}

func WhatsAppNotRegistered(phone string) {
	ErrorLogger.Printf("%s[%s] %s✗%s Number NOT registered on WhatsApp: %s",
		ColorGray, timestamp(),
		ColorRed, ColorReset, phone)
}

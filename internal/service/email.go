package service

import (
	"fmt"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// EmailRecipient represents a single email recipient.
type EmailRecipient struct {
	Email string
	Name  string
}

// EmailService handles sending emails via Gmail SMTP.
type EmailService struct {
	from     string
	password string
}

// NewEmailService creates a new EmailService reading credentials from environment variables.
func NewEmailService() *EmailService {
	return &EmailService{
		from:     os.Getenv("MAIL_FROM"),
		password: os.Getenv("GMAIL_APP_PASSWORD"),
	}
}

// SendOTPEmail sends a 5-digit verification code email for registration.
func (s *EmailService) SendOTPEmail(toEmail, userName, code string) error {
	subject := "Your BulkSend Verification Code: " + code

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="margin:0;padding:0;background-color:#f4f4f5;font-family:'Segoe UI',Arial,sans-serif;">
  <table width="100%%" cellpadding="0" cellspacing="0" style="background-color:#f4f4f5;padding:40px 0;">
    <tr><td align="center">
      <table width="560" cellpadding="0" cellspacing="0" style="background-color:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 2px 8px rgba(0,0,0,0.08);">
        <tr>
          <td style="background-color:#16a34a;padding:28px 40px;text-align:center;">
            <h1 style="margin:0;color:#ffffff;font-size:24px;font-weight:700;">WhatsApp Bulk Sender</h1>
          </td>
        </tr>
        <tr>
          <td style="padding:40px;text-align:center;">
            <h2 style="margin:0 0 8px;color:#18181b;font-size:20px;">Verify your email address</h2>
            <p style="margin:0 0 32px;color:#71717a;font-size:15px;">Hi %s, use the code below to complete your registration.</p>
            <div style="display:inline-block;background-color:#f0fdf4;border:2px dashed #16a34a;border-radius:12px;padding:24px 48px;margin-bottom:32px;">
              <span style="font-size:42px;font-weight:800;letter-spacing:12px;color:#15803d;font-family:monospace;">%s</span>
            </div>
            <p style="margin:0 0 8px;color:#71717a;font-size:13px;">This code expires in <strong>10 minutes</strong>.</p>
            <p style="margin:0;color:#a1a1aa;font-size:12px;">If you did not request this, you can safely ignore this email.</p>
          </td>
        </tr>
        <tr>
          <td style="background-color:#fafafa;padding:20px 40px;border-top:1px solid #e4e4e7;text-align:center;">
            <p style="margin:0;color:#a1a1aa;font-size:12px;">WhatsApp Bulk Sender &mdash; Secure Email Verification</p>
          </td>
        </tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, userName, code)

	return s.sendMail(toEmail, subject, htmlBody)
}

// SendPasswordResetEmail sends a 5-digit reset code to the user's email.
func (s *EmailService) SendPasswordResetEmail(toEmail, userName, code string) error {
	subject := "Reset Your BulkSend Password — Code: " + code

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="margin:0;padding:0;background-color:#f4f4f5;font-family:'Segoe UI',Arial,sans-serif;">
  <table width="100%%" cellpadding="0" cellspacing="0" style="background-color:#f4f4f5;padding:40px 0;">
    <tr><td align="center">
      <table width="560" cellpadding="0" cellspacing="0" style="background-color:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 2px 8px rgba(0,0,0,0.08);">
        <tr>
          <td style="background-color:#dc2626;padding:28px 40px;text-align:center;">
            <h1 style="margin:0;color:#ffffff;font-size:24px;font-weight:700;">WhatsApp Bulk Sender</h1>
          </td>
        </tr>
        <tr>
          <td style="padding:40px;text-align:center;">
            <h2 style="margin:0 0 8px;color:#18181b;font-size:20px;">Password Reset Request</h2>
            <p style="margin:0 0 32px;color:#71717a;font-size:15px;">Hi %s, use the code below to reset your password.</p>
            <div style="display:inline-block;background-color:#fef2f2;border:2px dashed #dc2626;border-radius:12px;padding:24px 48px;margin-bottom:32px;">
              <span style="font-size:42px;font-weight:800;letter-spacing:12px;color:#b91c1c;font-family:monospace;">%s</span>
            </div>
            <p style="margin:0 0 8px;color:#71717a;font-size:13px;">This code expires in <strong>10 minutes</strong>.</p>
            <p style="margin:0;color:#a1a1aa;font-size:12px;">If you did not request a password reset, you can safely ignore this email. Your password will not change.</p>
          </td>
        </tr>
        <tr>
          <td style="background-color:#fafafa;padding:20px 40px;border-top:1px solid #e4e4e7;text-align:center;">
            <p style="margin:0;color:#a1a1aa;font-size:12px;">WhatsApp Bulk Sender &mdash; Account Security</p>
          </td>
        </tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, userName, code)

	return s.sendMail(toEmail, subject, htmlBody)
}

// SendWelcomeEmail sends a branded HTML welcome email after user registration.
func (s *EmailService) SendWelcomeEmail(toEmail, userName string) error {
	subject := "Welcome to WhatsApp Bulk Sender — Your Free Trial Starts Now!"

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="margin:0;padding:0;background-color:#f4f4f5;font-family:'Segoe UI',Arial,sans-serif;">
  <table width="100%%" cellpadding="0" cellspacing="0" style="background-color:#f4f4f5;padding:40px 0;">
    <tr><td align="center">
      <table width="600" cellpadding="0" cellspacing="0" style="background-color:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 2px 8px rgba(0,0,0,0.08);">
        <!-- Header -->
        <tr>
          <td style="background-color:#16a34a;padding:32px 40px;text-align:center;">
            <h1 style="margin:0;color:#ffffff;font-size:26px;font-weight:700;">WhatsApp Bulk Sender</h1>
          </td>
        </tr>
        <!-- Body -->
        <tr>
          <td style="padding:40px;">
            <h2 style="margin:0 0 16px;color:#18181b;font-size:22px;">Welcome aboard, %s!</h2>
            <p style="margin:0 0 16px;color:#3f3f46;font-size:16px;line-height:1.6;">
              We're excited to have you. Your account is now active with a <strong>free trial of 5 bulk messages</strong> that gives you full access to every feature.
            </p>
            <p style="margin:0 0 8px;color:#3f3f46;font-size:16px;line-height:1.6;">Here's what you can do right away:</p>
            <ul style="margin:0 0 24px;padding-left:20px;color:#3f3f46;font-size:15px;line-height:1.8;">
              <li>Import contacts from Excel or CSV files</li>
              <li>Compose and send messages with images &amp; templates</li>
              <li>Track delivery status in real-time</li>
              <li>Use pre-built message templates</li>
            </ul>
            <table cellpadding="0" cellspacing="0" style="margin:0 auto 24px;">
              <tr><td style="background-color:#16a34a;border-radius:8px;padding:14px 32px;text-align:center;">
                <a href="https://bulksender.todayintech.in" style="color:#ffffff;text-decoration:none;font-size:16px;font-weight:600;">Start Sending Messages</a>
              </td></tr>
            </table>
            <p style="margin:0;color:#71717a;font-size:14px;line-height:1.5;">
              Your free trial includes 5 messages. Upgrade anytime for unlimited messaging.
            </p>
          </td>
        </tr>
        <!-- Footer -->
        <tr>
          <td style="background-color:#fafafa;padding:24px 40px;border-top:1px solid #e4e4e7;text-align:center;">
            <p style="margin:0 0 4px;color:#a1a1aa;font-size:12px;">WhatsApp Bulk Sender</p>
            <p style="margin:0;color:#a1a1aa;font-size:12px;">If you no longer wish to receive these emails, reply with "unsubscribe".</p>
          </td>
        </tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, userName)

	return s.sendMail(toEmail, subject, htmlBody)
}

// InvoiceData holds the details needed to generate a payment invoice email.
type InvoiceData struct {
	UserName    string
	UserEmail   string
	Plan        string
	Amount      string
	TxnID       string
	PaymentID   string
	PaymentDate time.Time
	ExpiryDate  time.Time
}

// SendInvoiceEmail sends a professional invoice email after successful payment.
func (s *EmailService) SendInvoiceEmail(data InvoiceData) error {
	subject := fmt.Sprintf("Payment Receipt — Invoice for %s Plan", strings.Title(data.Plan))
	invoiceNo := fmt.Sprintf("INV-%s", data.TxnID[len(data.TxnID)-min(8, len(data.TxnID)):])
	payDate := data.PaymentDate.Format("02 Jan 2006")
	expDate := data.ExpiryDate.Format("02 Jan 2006")
	planLabel := strings.Title(data.Plan)

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="margin:0;padding:0;background-color:#f4f4f5;font-family:'Segoe UI',Arial,sans-serif;">
  <table width="100%%%%" cellpadding="0" cellspacing="0" style="background-color:#f4f4f5;padding:40px 0;">
    <tr><td align="center">
      <table width="600" cellpadding="0" cellspacing="0" style="background-color:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 2px 8px rgba(0,0,0,0.08);">
        <!-- Header -->
        <tr>
          <td style="background-color:#16a34a;padding:28px 40px;">
            <table width="100%%%%" cellpadding="0" cellspacing="0">
              <tr>
                <td><h1 style="margin:0;color:#ffffff;font-size:24px;font-weight:700;">WhatsApp Bulk Sender</h1></td>
                <td style="text-align:right;"><span style="color:#bbf7d0;font-size:14px;font-weight:600;">INVOICE</span></td>
              </tr>
            </table>
          </td>
        </tr>
        <!-- Invoice Info -->
        <tr>
          <td style="padding:32px 40px 0;">
            <table width="100%%%%" cellpadding="0" cellspacing="0">
              <tr>
                <td style="vertical-align:top;">
                  <p style="margin:0 0 4px;color:#71717a;font-size:12px;text-transform:uppercase;letter-spacing:0.5px;">Billed To</p>
                  <p style="margin:0 0 2px;color:#18181b;font-size:15px;font-weight:600;">%s</p>
                  <p style="margin:0;color:#71717a;font-size:14px;">%s</p>
                </td>
                <td style="vertical-align:top;text-align:right;">
                  <p style="margin:0 0 4px;color:#71717a;font-size:12px;text-transform:uppercase;letter-spacing:0.5px;">Invoice Details</p>
                  <p style="margin:0 0 2px;color:#18181b;font-size:14px;"><strong>Invoice:</strong> %s</p>
                  <p style="margin:0 0 2px;color:#18181b;font-size:14px;"><strong>Date:</strong> %s</p>
                  <p style="margin:0;color:#18181b;font-size:14px;"><strong>Payment ID:</strong> %s</p>
                </td>
              </tr>
            </table>
          </td>
        </tr>
        <!-- Line Items -->
        <tr>
          <td style="padding:28px 40px;">
            <table width="100%%%%" cellpadding="0" cellspacing="0" style="border:1px solid #e4e4e7;border-radius:8px;overflow:hidden;">
              <tr style="background-color:#f4f4f5;">
                <td style="padding:12px 16px;color:#71717a;font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;border-bottom:1px solid #e4e4e7;">Description</td>
                <td style="padding:12px 16px;color:#71717a;font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;border-bottom:1px solid #e4e4e7;text-align:center;">Period</td>
                <td style="padding:12px 16px;color:#71717a;font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px;border-bottom:1px solid #e4e4e7;text-align:right;">Amount</td>
              </tr>
              <tr>
                <td style="padding:16px;color:#18181b;font-size:15px;font-weight:500;border-bottom:1px solid #e4e4e7;">BulkSend %s Plan</td>
                <td style="padding:16px;color:#3f3f46;font-size:14px;border-bottom:1px solid #e4e4e7;text-align:center;">%s — %s</td>
                <td style="padding:16px;color:#18181b;font-size:15px;font-weight:600;border-bottom:1px solid #e4e4e7;text-align:right;">₹%s</td>
              </tr>
              <tr style="background-color:#f0fdf4;">
                <td colspan="2" style="padding:14px 16px;color:#18181b;font-size:15px;font-weight:700;">Total Paid</td>
                <td style="padding:14px 16px;color:#16a34a;font-size:18px;font-weight:700;text-align:right;">₹%s</td>
              </tr>
            </table>
          </td>
        </tr>
        <!-- Transaction Details -->
        <tr>
          <td style="padding:0 40px 28px;">
            <table width="100%%%%" cellpadding="0" cellspacing="0" style="background-color:#fafafa;border-radius:8px;padding:16px;">
              <tr>
                <td style="padding:8px 16px;color:#71717a;font-size:13px;">Transaction ID</td>
                <td style="padding:8px 16px;color:#18181b;font-size:13px;font-family:monospace;text-align:right;">%s</td>
              </tr>
              <tr>
                <td style="padding:8px 16px;color:#71717a;font-size:13px;">Payment Status</td>
                <td style="padding:8px 16px;text-align:right;"><span style="color:#16a34a;font-size:13px;font-weight:600;">✓ Successful</span></td>
              </tr>
              <tr>
                <td style="padding:8px 16px;color:#71717a;font-size:13px;">Subscription Valid Until</td>
                <td style="padding:8px 16px;color:#18181b;font-size:13px;font-weight:600;text-align:right;">%s</td>
              </tr>
            </table>
          </td>
        </tr>
        <!-- CTA -->
        <tr>
          <td style="padding:0 40px 32px;text-align:center;">
            <table cellpadding="0" cellspacing="0" style="margin:0 auto;">
              <tr><td style="background-color:#16a34a;border-radius:8px;padding:14px 32px;text-align:center;">
                <a href="%s/app" style="color:#ffffff;text-decoration:none;font-size:16px;font-weight:600;">Go to Dashboard</a>
              </td></tr>
            </table>
          </td>
        </tr>
        <!-- Footer -->
        <tr>
          <td style="background-color:#fafafa;padding:24px 40px;border-top:1px solid #e4e4e7;">
            <p style="margin:0 0 8px;color:#71717a;font-size:13px;text-align:center;">
              This is an auto-generated invoice for your records. No signature required.
            </p>
            <p style="margin:0 0 4px;color:#a1a1aa;font-size:12px;text-align:center;">WhatsApp Bulk Sender</p>
            <p style="margin:0;color:#a1a1aa;font-size:12px;text-align:center;">If you no longer wish to receive these emails, reply with "unsubscribe".</p>
          </td>
        </tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`,
		data.UserName, data.UserEmail,
		invoiceNo, payDate, data.PaymentID,
		planLabel, payDate, expDate, data.Amount,
		data.Amount,
		data.TxnID,
		expDate,
		os.Getenv("FRONTEND_URL"),
	)

	return s.sendMail(data.UserEmail, subject, htmlBody)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SendPromotionalEmail sends a promotional email with custom subject and HTML body,
// wrapped in the standard branded layout.
func (s *EmailService) SendPromotionalEmail(toEmail, userName, subject, htmlBody string) error {
	wrappedBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="margin:0;padding:0;background-color:#f4f4f5;font-family:'Segoe UI',Arial,sans-serif;">
  <table width="100%%" cellpadding="0" cellspacing="0" style="background-color:#f4f4f5;padding:40px 0;">
    <tr><td align="center">
      <table width="600" cellpadding="0" cellspacing="0" style="background-color:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 2px 8px rgba(0,0,0,0.08);">
        <!-- Header -->
        <tr>
          <td style="background-color:#16a34a;padding:32px 40px;text-align:center;">
            <h1 style="margin:0;color:#ffffff;font-size:26px;font-weight:700;">WhatsApp Bulk Sender</h1>
          </td>
        </tr>
        <!-- Body -->
        <tr>
          <td style="padding:40px;">
            <h2 style="margin:0 0 16px;color:#18181b;font-size:22px;">Hi %s,</h2>
            %s
          </td>
        </tr>
        <!-- Footer -->
        <tr>
          <td style="background-color:#fafafa;padding:24px 40px;border-top:1px solid #e4e4e7;text-align:center;">
            <p style="margin:0 0 4px;color:#a1a1aa;font-size:12px;">WhatsApp Bulk Sender</p>
            <p style="margin:0;color:#a1a1aa;font-size:12px;">If you no longer wish to receive these emails, reply with "unsubscribe".</p>
          </td>
        </tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, userName, htmlBody)

	return s.sendMail(toEmail, subject, wrappedBody)
}

// SendBulkPromotionalEmail sends a promotional email to multiple recipients.
// It returns a slice of errors corresponding to each recipient (nil if successful).
func (s *EmailService) SendBulkPromotionalEmail(recipients []EmailRecipient, subject, htmlBody string) []error {
	errs := make([]error, len(recipients))
	for i, r := range recipients {
		errs[i] = s.SendPromotionalEmail(r.Email, r.Name, subject, htmlBody)
	}
	return errs
}

// sendMail delivers an HTML email via Gmail SMTP with STARTTLS.
func (s *EmailService) sendMail(to, subject, htmlBody string) error {
	if s.from == "" || s.password == "" {
		return fmt.Errorf("email service not configured: MAIL_FROM or GMAIL_APP_PASSWORD is missing")
	}

	smtpHost := "smtp.gmail.com"
	smtpAddr := smtpHost + ":587"

	headers := strings.Join([]string{
		"From: " + s.from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
	}, "\r\n")

	msg := []byte(headers + "\r\n\r\n" + htmlBody)

	auth := smtp.PlainAuth("", s.from, s.password, smtpHost)

	return smtp.SendMail(smtpAddr, auth, s.from, []string{to}, msg)
}

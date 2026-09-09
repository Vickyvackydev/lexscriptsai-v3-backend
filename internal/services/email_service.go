package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"lexscriptsai-v3-backend/internal/config"
)

type EmailService struct {
	cfg        *config.Config
	httpClient *http.Client
}

func NewEmailService(cfg *config.Config) *EmailService {
	return &EmailService{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 12 * time.Second,
		},
	}
}

type brevoRecipient struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type brevoSender struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type brevoPayload struct {
	Sender      brevoSender      `json:"sender"`
	To          []brevoRecipient `json:"to"`
	Subject     string           `json:"subject"`
	HTMLContent string           `json:"htmlContent"`
}

func (s *EmailService) SendEmail(toEmail, toName, subject, htmlContent string) error {
	if s.cfg.BrevoAPIKey == "" {
		log.Printf("[EmailService] (NO BREVO_API_KEY CONFIGURED) Mock email to %s (%s). Subject: %s", toEmail, toName, subject)
		return nil
	}

	senderEmail := s.cfg.BrevoSenderEmail
	if senderEmail == "" {
		senderEmail = "noreply@lexscriptsai.com"
	}
	senderName := s.cfg.BrevoSenderName
	if senderName == "" {
		senderName = "LexScriptsAI"
	}

	payload := brevoPayload{
		Sender: brevoSender{
			Email: senderEmail,
			Name:  senderName,
		},
		To: []brevoRecipient{
			{
				Email: toEmail,
				Name:  toName,
			},
		},
		Subject:     subject,
		HTMLContent: htmlContent,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal email payload: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.brevo.com/v3/smtp/email", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create Brevo request: %w", err)
	}

	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("api-key", s.cfg.BrevoAPIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute Brevo HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("brevo API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	log.Printf("[EmailService] Successfully sent email to %s via Brevo API", toEmail)
	return nil
}

func (s *EmailService) SendWelcomeEmail(toEmail, toName, password, role, loginURL string) {
	subject := "Your LexScriptsAI V3 Account Credentials"
	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Welcome to LexScriptsAI V3</title>
</head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #0d1117; color: #c9d1d9; margin: 0; padding: 32px 16px;">
  <div style="max-width: 540px; margin: 0 auto; background-color: #161b22; border: 1px solid #30363d; border-radius: 8px; overflow: hidden; box-shadow: 0 8px 24px rgba(0,0,0,0.5);">
    <div style="padding: 24px 32px; border-bottom: 1px solid #30363d; background-color: #0d1117;">
      <h2 style="margin: 0; color: #58a6ff; font-size: 20px; font-weight: 700; letter-spacing: -0.3px;">LexScriptsAI V3</h2>
      <p style="margin: 4px 0 0 0; color: #8b949e; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px;">Court-Grade Legal Transcription</p>
    </div>
    <div style="padding: 32px;">
      <h3 style="margin: 0 0 16px 0; color: #f0f6fc; font-size: 18px; font-weight: 600;">Welcome, %s</h3>
      <p style="margin: 0 0 20px 0; font-size: 14px; line-height: 1.6; color: #8b949e;">
        You have been registered as an authorized user on the LexScriptsAI V3 Court Transcription Platform. Below are your account sign-in credentials:
      </p>

      <div style="background-color: #0d1117; border: 1px solid #30363d; border-radius: 6px; padding: 18px; margin-bottom: 24px;">
        <table style="width: 100%%; font-size: 13px; border-collapse: collapse;">
          <tr>
            <td style="color: #8b949e; padding: 4px 0; width: 100px;">Email:</td>
            <td style="color: #f0f6fc; font-weight: 600; font-family: monospace;">%s</td>
          </tr>
          <tr>
            <td style="color: #8b949e; padding: 4px 0;">Role:</td>
            <td style="color: #f0f6fc; font-weight: 600;">%s</td>
          </tr>
          <tr>
            <td style="color: #8b949e; padding: 4px 0;">Password:</td>
            <td style="color: #58a6ff; font-weight: 700; font-family: monospace; font-size: 14px;">%s</td>
          </tr>
        </table>
      </div>

      <div style="text-align: center; margin: 32px 0;">
        <a href="%s" style="background-color: #238636; color: #ffffff; text-decoration: none; padding: 12px 28px; border-radius: 6px; font-size: 14px; font-weight: 600; display: inline-block;">
          Sign In to LexScriptsAI
        </a>
      </div>

      <p style="margin: 24px 0 0 0; font-size: 12px; color: #8b949e; line-height: 1.5; border-top: 1px solid #30363d; padding-top: 16px;">
        <strong>Security Notice:</strong> Please sign in and immediately update your temporary password in your account settings. If you did not expect this invitation, please contact your administrator immediately.
      </p>
    </div>
  </div>
</body>
</html>
`, toName, toEmail, role, password, loginURL)

	if err := s.SendEmail(toEmail, toName, subject, html); err != nil {
		log.Printf("[EmailService] Failed to send welcome email to %s: %v", toEmail, err)
	}
}

func (s *EmailService) SendPasswordResetLink(toEmail, toName, resetLink string) {
	subject := "Reset Your LexScriptsAI V3 Password"
	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Reset Your Password - LexScriptsAI</title>
</head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #0d1117; color: #c9d1d9; margin: 0; padding: 32px 16px;">
  <div style="max-width: 540px; margin: 0 auto; background-color: #161b22; border: 1px solid #30363d; border-radius: 8px; overflow: hidden; box-shadow: 0 8px 24px rgba(0,0,0,0.5);">
    <div style="padding: 24px 32px; border-bottom: 1px solid #30363d; background-color: #0d1117;">
      <h2 style="margin: 0; color: #58a6ff; font-size: 20px; font-weight: 700; letter-spacing: -0.3px;">LexScriptsAI V3</h2>
      <p style="margin: 4px 0 0 0; color: #8b949e; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px;">Password Reset Request</p>
    </div>
    <div style="padding: 32px;">
      <h3 style="margin: 0 0 16px 0; color: #f0f6fc; font-size: 18px; font-weight: 600;">Hello %s,</h3>
      <p style="margin: 0 0 20px 0; font-size: 14px; line-height: 1.6; color: #8b949e;">
        We received a request to reset your password for your LexScriptsAI account (%s). Click the secure link below to verify your email and choose a new password:
      </p>

      <div style="text-align: center; margin: 32px 0;">
        <a href="%s" style="background-color: #1f6feb; color: #ffffff; text-decoration: none; padding: 12px 28px; border-radius: 6px; font-size: 14px; font-weight: 600; display: inline-block;">
          Verify Email & Change Password
        </a>
      </div>

      <p style="margin: 0 0 16px 0; font-size: 12px; color: #8b949e; line-height: 1.5;">
        Or copy and paste this link into your browser:
      </p>
      <div style="background-color: #0d1117; border: 1px solid #30363d; border-radius: 4px; padding: 10px; font-family: monospace; font-size: 11px; word-break: break-all; color: #58a6ff; margin-bottom: 24px;">
        %s
      </div>

      <p style="margin: 24px 0 0 0; font-size: 12px; color: #8b949e; line-height: 1.5; border-top: 1px solid #30363d; padding-top: 16px;">
        This reset link will expire in <strong>1 hour</strong>. If you did not request this password reset, no action is needed and your password remains unchanged.
      </p>
    </div>
  </div>
</body>
</html>
`, toName, toEmail, resetLink, resetLink)

	if err := s.SendEmail(toEmail, toName, subject, html); err != nil {
		log.Printf("[EmailService] Failed to send password reset email to %s: %v", toEmail, err)
	}
}

func (s *EmailService) SendPasswordResetRequestAcknowledged(toEmail, toName string) {
	subject := "Password Reset Request Received — LexScriptsAI"
	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Password Reset Request Received</title>
</head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #0d1117; color: #c9d1d9; margin: 0; padding: 32px 16px;">
  <div style="max-width: 540px; margin: 0 auto; background-color: #161b22; border: 1px solid #30363d; border-radius: 8px; overflow: hidden; box-shadow: 0 8px 24px rgba(0,0,0,0.5);">
    <div style="padding: 24px 32px; border-bottom: 1px solid #30363d; background-color: #0d1117;">
      <h2 style="margin: 0; color: #d97706; font-size: 20px; font-weight: 700; letter-spacing: -0.3px;">LexScriptsAI V3</h2>
      <p style="margin: 4px 0 0 0; color: #8b949e; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px;">Password Reset Notice</p>
    </div>
    <div style="padding: 32px;">
      <h3 style="margin: 0 0 16px 0; color: #f0f6fc; font-size: 18px; font-weight: 600;">Hello %s,</h3>
      <p style="margin: 0 0 16px 0; font-size: 14px; line-height: 1.6; color: #c9d1d9;">
        We have received your request to reset the password for your account (<strong>%s</strong>).
      </p>
      <div style="background-color: rgba(217, 119, 6, 0.1); border: 1px solid rgba(217, 119, 6, 0.3); border-radius: 6px; padding: 16px; margin: 20px 0;">
        <p style="margin: 0; font-size: 13px; color: #fbbf24; line-height: 1.5;">
          <strong>Security Policy:</strong> In accordance with our judicial security protocols, password resets are processed and verified directly by the System Administrator.
        </p>
      </div>
      <p style="margin: 0 0 16px 0; font-size: 13px; line-height: 1.6; color: #8b949e;">
        Your request has been logged and the System Administrator has been notified. You will receive an automated email as soon as the administrator updates your password with your new credentials.
      </p>
      <p style="margin: 24px 0 0 0; font-size: 12px; color: #8b949e; line-height: 1.5; border-top: 1px solid #30363d; padding-top: 16px;">
        If you did not make this request, please alert your organization's administrator immediately.
      </p>
    </div>
  </div>
</body>
</html>
`, toName, toEmail)

	if err := s.SendEmail(toEmail, toName, subject, html); err != nil {
		log.Printf("[EmailService] Failed to send reset acknowledgment to %s: %v", toEmail, err)
	}
}

func (s *EmailService) SendAdminPasswordResetAlert(adminEmail, userName, userEmail, requestTime, adminConsoleURL string) {
	subject := fmt.Sprintf("Action Required: Password Reset Request for %s (%s)", userName, userEmail)
	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Admin Action Required: Password Reset Request</title>
</head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #0d1117; color: #c9d1d9; margin: 0; padding: 32px 16px;">
  <div style="max-width: 560px; margin: 0 auto; background-color: #161b22; border: 1px solid #30363d; border-radius: 8px; overflow: hidden; box-shadow: 0 8px 24px rgba(0,0,0,0.5);">
    <div style="padding: 24px 32px; border-bottom: 1px solid #30363d; background-color: #0d1117;">
      <h2 style="margin: 0; color: #ef4444; font-size: 20px; font-weight: 700; letter-spacing: -0.3px;">LexScriptsAI Admin Alert</h2>
      <p style="margin: 4px 0 0 0; color: #8b949e; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px;">User Password Reset Request</p>
    </div>
    <div style="padding: 32px;">
      <h3 style="margin: 0 0 16px 0; color: #f0f6fc; font-size: 17px; font-weight: 600;">Administrator Action Required</h3>
      <p style="margin: 0 0 20px 0; font-size: 14px; line-height: 1.6; color: #c9d1d9;">
        A user has requested a password reset on the LexScriptsAI portal. Because self-service reset is disabled, this user is waiting for you to update their password.
      </p>

      <div style="background-color: #0d1117; border: 1px solid #30363d; border-radius: 6px; padding: 16px; margin: 20px 0;">
        <table style="width: 100%%; font-size: 13px; color: #c9d1d9; border-collapse: collapse;">
          <tr>
            <td style="padding: 6px 0; color: #8b949e; width: 120px;">User Name:</td>
            <td style="padding: 6px 0; font-weight: 600; color: #f0f6fc;">%s</td>
          </tr>
          <tr>
            <td style="padding: 6px 0; color: #8b949e;">User Email:</td>
            <td style="padding: 6px 0; font-weight: 600; color: #58a6ff;">%s</td>
          </tr>
          <tr>
            <td style="padding: 6px 0; color: #8b949e;">Requested At:</td>
            <td style="padding: 6px 0; color: #c9d1d9;">%s</td>
          </tr>
        </table>
      </div>

      <div style="text-align: center; margin: 28px 0;">
        <a href="%s" style="background-color: #d97706; color: #ffffff; text-decoration: none; padding: 12px 24px; border-radius: 6px; font-size: 13px; font-weight: 600; display: inline-block;">
          Open Admin Console to Reset Password
        </a>
      </div>

      <p style="margin: 20px 0 0 0; font-size: 12px; color: #8b949e; line-height: 1.5; border-top: 1px solid #30363d; padding-top: 16px;">
        You can also reset this user's password anytime directly from the <strong>Accounts / Users</strong> table or the topbar <strong>Notifications</strong> menu.
      </p>
    </div>
  </div>
</body>
</html>
`, userName, userEmail, requestTime, adminConsoleURL)

	if err := s.SendEmail(adminEmail, "LexScriptsAI Administrator", subject, html); err != nil {
		log.Printf("[EmailService] Failed to send admin password reset alert to %s: %v", adminEmail, err)
	}
}

func (s *EmailService) SendPasswordUpdatedNotification(toEmail, toName, newPassword, loginURL string) {
	subject := "Your Account Password Has Been Updated — LexScriptsAI"
	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Your Password Has Been Updated</title>
</head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #0d1117; color: #c9d1d9; margin: 0; padding: 32px 16px;">
  <div style="max-width: 540px; margin: 0 auto; background-color: #161b22; border: 1px solid #30363d; border-radius: 8px; overflow: hidden; box-shadow: 0 8px 24px rgba(0,0,0,0.5);">
    <div style="padding: 24px 32px; border-bottom: 1px solid #30363d; background-color: #0d1117;">
      <h2 style="margin: 0; color: #10b981; font-size: 20px; font-weight: 700; letter-spacing: -0.3px;">LexScriptsAI V3</h2>
      <p style="margin: 4px 0 0 0; color: #8b949e; font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px;">Password Update Confirmation</p>
    </div>
    <div style="padding: 32px;">
      <h3 style="margin: 0 0 16px 0; color: #f0f6fc; font-size: 18px; font-weight: 600;">Hello %s,</h3>
      <p style="margin: 0 0 16px 0; font-size: 14px; line-height: 1.6; color: #c9d1d9;">
        Your password has been successfully updated by the System Administrator. You can now use your new credentials below to sign in:
      </p>

      <div style="background-color: #0d1117; border: 1px solid #30363d; border-radius: 6px; padding: 18px; margin: 20px 0;">
        <table style="width: 100%%; font-size: 13px; color: #c9d1d9; border-collapse: collapse;">
          <tr>
            <td style="padding: 6px 0; color: #8b949e; width: 120px;">Email:</td>
            <td style="padding: 6px 0; font-weight: 600; color: #f0f6fc;">%s</td>
          </tr>
          <tr>
            <td style="padding: 6px 0; color: #8b949e;">New Password:</td>
            <td style="padding: 6px 0; font-family: monospace; font-size: 14px; font-weight: 700; color: #10b981;">%s</td>
          </tr>
        </table>
      </div>

      <div style="text-align: center; margin: 28px 0;">
        <a href="%s" style="background-color: #10b981; color: #ffffff; text-decoration: none; padding: 12px 28px; border-radius: 6px; font-size: 14px; font-weight: 600; display: inline-block;">
          Sign In to LexScriptsAI
        </a>
      </div>

      <p style="margin: 24px 0 0 0; font-size: 12px; color: #8b949e; line-height: 1.5; border-top: 1px solid #30363d; padding-top: 16px;">
        <strong>Security Recommendation:</strong> After logging in, you can update your password at any time in your profile settings.
      </p>
    </div>
  </div>
</body>
</html>
`, toName, toEmail, newPassword, loginURL)

	if err := s.SendEmail(toEmail, toName, subject, html); err != nil {
		log.Printf("[EmailService] Failed to send password updated email to %s: %v", toEmail, err)
	}
}

func (s *EmailService) SendTranscriptCompletedEmail(toEmail, toName, transcriptTitle, transcriptID string, durationSec int) {
	subject := fmt.Sprintf("Transcription Completed: %s", transcriptTitle)
	editorURL := fmt.Sprintf("%s/transcripts/%s", s.cfg.FrontendURL, transcriptID)
	m := durationSec / 60
	sec := durationSec % 60
	durationFormatted := fmt.Sprintf("%02d:%02d", m, sec)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #0d1117; color: #c9d1d9; margin: 0; padding: 32px 16px;">
  <div style="max-width: 540px; margin: 0 auto; background-color: #161b22; border: 1px solid #30363d; border-radius: 8px; overflow: hidden; box-shadow: 0 8px 24px rgba(0,0,0,0.5);">
    <div style="padding: 24px 32px; border-bottom: 1px solid #30363d; background-color: #0d1117;">
      <h2 style="margin: 0; color: #10b981; font-size: 20px; font-weight: 700;">LexScriptsAI</h2>
      <p style="margin: 4px 0 0 0; color: #8b949e; font-size: 12px; text-transform: uppercase;">Transcription Ready</p>
    </div>
    <div style="padding: 32px;">
      <h3 style="margin: 0 0 16px 0; color: #f0f6fc; font-size: 18px;">Hello %s,</h3>
      <p style="margin: 0 0 16px 0; font-size: 14px; line-height: 1.6; color: #c9d1d9;">
        Your audio transcription for <strong>%s</strong> has completed successfully. Diarized speaker banks and word timestamps are now ready for review and editing.
      </p>

      <div style="background-color: #0d1117; border: 1px solid #30363d; border-radius: 6px; padding: 16px; margin: 20px 0;">
        <p style="margin: 0 0 8px 0; font-size: 13px; color: #8b949e;">Duration: <strong style="color: #f0f6fc;">%s</strong></p>
        <p style="margin: 0; font-size: 13px; color: #8b949e;">Status: <strong style="color: #10b981;">Completed</strong></p>
      </div>

      <div style="text-align: center; margin: 28px 0;">
        <a href="%s" style="background-color: #2563eb; color: #ffffff; text-decoration: none; padding: 12px 28px; border-radius: 6px; font-size: 14px; font-weight: 600; display: inline-block;">
          Open in Transcript Editor
        </a>
      </div>
    </div>
  </div>
</body>
</html>`, toName, transcriptTitle, durationFormatted, editorURL)

	if err := s.SendEmail(toEmail, toName, subject, html); err != nil {
		log.Printf("[EmailService] Failed to send transcription completed email to %s: %v", toEmail, err)
	}
}

func (s *EmailService) SendTranscriptFailedEmail(toEmail, toName, transcriptTitle, reason string) {
	subject := fmt.Sprintf("Transcription Failed: %s", transcriptTitle)
	transcriptsURL := fmt.Sprintf("%s/transcripts", s.cfg.FrontendURL)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #0d1117; color: #c9d1d9; margin: 0; padding: 32px 16px;">
  <div style="max-width: 540px; margin: 0 auto; background-color: #161b22; border: 1px solid #30363d; border-radius: 8px; overflow: hidden; box-shadow: 0 8px 24px rgba(0,0,0,0.5);">
    <div style="padding: 24px 32px; border-bottom: 1px solid #30363d; background-color: #0d1117;">
      <h2 style="margin: 0; color: #ef4444; font-size: 20px; font-weight: 700;">LexScriptsAI</h2>
      <p style="margin: 4px 0 0 0; color: #8b949e; font-size: 12px; text-transform: uppercase;">Transcription Processing Notice</p>
    </div>
    <div style="padding: 32px;">
      <h3 style="margin: 0 0 16px 0; color: #f0f6fc; font-size: 18px;">Hello %s,</h3>
      <p style="margin: 0 0 16px 0; font-size: 14px; line-height: 1.6; color: #c9d1d9;">
        We encountered an issue while transcribing <strong>%s</strong>:
      </p>

      <div style="background-color: #0d1117; border: 1px solid #ef4444; border-radius: 6px; padding: 16px; margin: 20px 0;">
        <p style="margin: 0; font-size: 13px; color: #ef4444;">%s</p>
      </div>

      <p style="margin: 16px 0; font-size: 13px; color: #8b949e;">
        You can retry processing this audio directly from your Transcripts list.
      </p>

      <div style="text-align: center; margin: 28px 0;">
        <a href="%s" style="background-color: #374151; color: #ffffff; text-decoration: none; padding: 12px 28px; border-radius: 6px; font-size: 14px; font-weight: 600; display: inline-block;">
          Go to Transcripts List
        </a>
      </div>
    </div>
  </div>
</body>
</html>`, toName, transcriptTitle, reason, transcriptsURL)

	if err := s.SendEmail(toEmail, toName, subject, html); err != nil {
		log.Printf("[EmailService] Failed to send transcription failed email to %s: %v", toEmail, err)
	}
}

func (s *EmailService) SendTranscriptShareEmail(toEmail, toName, ownerName, transcriptTitle, transcriptID, role string) {
	subject := fmt.Sprintf("%s shared a transcript with you: %s", ownerName, transcriptTitle)
	editorURL := fmt.Sprintf("%s/transcripts/%s", s.cfg.FrontendURL, transcriptID)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #0d1117; color: #c9d1d9; margin: 0; padding: 32px 16px;">
  <div style="max-width: 540px; margin: 0 auto; background-color: #161b22; border: 1px solid #30363d; border-radius: 8px; overflow: hidden; box-shadow: 0 8px 24px rgba(0,0,0,0.5);">
    <div style="padding: 24px 32px; border-bottom: 1px solid #30363d; background-color: #0d1117;">
      <h2 style="margin: 0; color: #3b82f6; font-size: 20px; font-weight: 700;">LexScriptsAI</h2>
      <p style="margin: 4px 0 0 0; color: #8b949e; font-size: 12px; text-transform: uppercase;">Collaboration Invite</p>
    </div>
    <div style="padding: 32px;">
      <h3 style="margin: 0 0 16px 0; color: #f0f6fc; font-size: 18px;">Hello %s,</h3>
      <p style="margin: 0 0 16px 0; font-size: 14px; line-height: 1.6; color: #c9d1d9;">
        <strong>%s</strong> has shared court transcript <strong>%s</strong> with you as a <strong>%s</strong>.
      </p>

      <div style="text-align: center; margin: 28px 0;">
        <a href="%s" style="background-color: #2563eb; color: #ffffff; text-decoration: none; padding: 12px 28px; border-radius: 6px; font-size: 14px; font-weight: 600; display: inline-block;">
          Open in Transcript Editor
        </a>
      </div>
    </div>
  </div>
</body>
</html>`, toName, ownerName, transcriptTitle, role, editorURL)

	if err := s.SendEmail(toEmail, toName, subject, html); err != nil {
		log.Printf("[EmailService] Failed to send transcript share email to %s: %v", toEmail, err)
	}
}



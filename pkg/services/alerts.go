package services

import (
	"fmt"

	"github.com/artpro/assessapp/pkg/config"
	"github.com/artpro/assessapp/pkg/models"
	"github.com/rs/zerolog"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

// AlertService handles sending alerts
type AlertService struct {
	cfg    *config.Config
	logger zerolog.Logger
}

// NewAlertService creates a new alert service
func NewAlertService(cfg *config.Config, logger zerolog.Logger) *AlertService {
	return &AlertService{
		cfg:    cfg,
		logger: logger,
	}
}

// SendAlert sends an email alert
func (s *AlertService) SendAlert(alert models.Alert) error {
	if s.cfg.SendGridAPIKey == "" {
		s.logger.Warn().Msg("SendGrid API key not configured, skipping email")
		return nil
	}

	from := mail.NewEmail("Stock Tracker Alerts", s.cfg.AlertEmailFrom)
	to := mail.NewEmail("Admin", s.cfg.AlertEmailTo)
	
	subject := fmt.Sprintf("Stock Alert: %s - %s", alert.Ticker, alert.AlertType)
	
	plainTextContent := fmt.Sprintf(
		"Alert for %s:\n\nType: %s\nMessage: %s\n\nGenerated at: %s",
		alert.Ticker,
		alert.AlertType,
		alert.Message,
		alert.CreatedAt.Format("2006-01-02 15:04:05"),
	)
	
	htmlContent := fmt.Sprintf(`
		<html>
		<body>
			<h2>Stock Alert: %s</h2>
			<p><strong>Type:</strong> %s</p>
			<p><strong>Message:</strong> %s</p>
			<p><strong>Time:</strong> %s</p>
		</body>
		</html>
	`, alert.Ticker, alert.AlertType, alert.Message, alert.CreatedAt.Format("2006-01-02 15:04:05"))

	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
	client := sendgrid.NewSendClient(s.cfg.SendGridAPIKey)
	
	response, err := client.Send(message)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	if response.StatusCode >= 300 {
		return fmt.Errorf("email service returned status %d", response.StatusCode)
	}

	s.logger.Info().Str("ticker", alert.Ticker).Msg("Alert email sent successfully")
	return nil
}


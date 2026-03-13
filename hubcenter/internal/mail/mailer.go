package mail

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
)

type Mailer interface {
	Send(ctx context.Context, to []string, subject string, body string) error
}

type smtpMailer struct {
	host      string
	port      int
	username  string
	password  string
	fromName  string
	fromEmail string
}

func New(cfg config.Config) Mailer {
	if !cfg.Mail.Enabled || strings.TrimSpace(cfg.Mail.SMTPHost) == "" {
		return nil
	}
	return &smtpMailer{
		host:      strings.TrimSpace(cfg.Mail.SMTPHost),
		port:      cfg.Mail.SMTPPort,
		username:  strings.TrimSpace(cfg.Mail.Username),
		password:  cfg.Mail.Password,
		fromName:  strings.TrimSpace(cfg.Mail.FromName),
		fromEmail: strings.TrimSpace(cfg.Mail.FromEmail),
	}
}

func (m *smtpMailer) Send(ctx context.Context, to []string, subject string, body string) error {
	_ = ctx
	if len(to) == 0 {
		return fmt.Errorf("mail recipient is required")
	}

	fromEmail := m.fromEmail
	if fromEmail == "" {
		fromEmail = m.username
	}
	if fromEmail == "" {
		return fmt.Errorf("mail sender is not configured")
	}

	headers := []string{
		"From: " + formatFrom(m.fromName, fromEmail),
		"To: " + strings.Join(to, ", "),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
	}
	message := strings.Join(headers, "\r\n") + body

	addr := fmt.Sprintf("%s:%d", m.host, m.port)
	var auth smtp.Auth
	if m.username != "" {
		auth = smtp.PlainAuth("", m.username, m.password, m.host)
	}
	return smtp.SendMail(addr, auth, fromEmail, to, []byte(message))
}

func formatFrom(name, email string) string {
	if strings.TrimSpace(name) == "" {
		return email
	}
	return fmt.Sprintf("%s <%s>", name, email)
}

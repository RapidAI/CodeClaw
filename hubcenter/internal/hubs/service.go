package hubs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

var ErrHubUnauthorized = errors.New("hub unauthorized")
var ErrEmailBlocked = errors.New("email blocked")
var ErrIPBlocked = errors.New("ip blocked")

type BlockedEmailRepository interface {
	GetByEmail(ctx context.Context, email string) (*store.BlockedEmail, error)
	Create(ctx context.Context, item *store.BlockedEmail) error
	DeleteByEmail(ctx context.Context, email string) error
	List(ctx context.Context) ([]*store.BlockedEmail, error)
}

type BlockedIPRepository interface {
	GetByIP(ctx context.Context, ip string) (*store.BlockedIP, error)
	Create(ctx context.Context, item *store.BlockedIP) error
	DeleteByIP(ctx context.Context, ip string) error
	List(ctx context.Context) ([]*store.BlockedIP, error)
}

type RegisterHubRequest struct {
	InstallationID string         `json:"installation_id"`
	OwnerEmail     string         `json:"owner_email"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	BaseURL        string         `json:"base_url"`
	Host           string         `json:"host"`
	Port           int            `json:"port"`
	Visibility     string         `json:"visibility"`
	EnrollmentMode string         `json:"enrollment_mode"`
	Capabilities   map[string]any `json:"capabilities"`
}

type RegisterHubResult struct {
	HubID     string `json:"hub_id"`
	HubSecret string `json:"hub_secret"`
}

type Service struct {
	hubs          store.HubRepository
	links         store.HubUserLinkRepository
	blockedEmails BlockedEmailRepository
	blockedIPs    BlockedIPRepository
}

func NewService(hubs store.HubRepository, links store.HubUserLinkRepository, blockedEmails BlockedEmailRepository, blockedIPs BlockedIPRepository) *Service {
	return &Service{
		hubs:          hubs,
		links:         links,
		blockedEmails: blockedEmails,
		blockedIPs:    blockedIPs,
	}
}

func (s *Service) RegisterHub(ctx context.Context, req RegisterHubRequest) (*RegisterHubResult, error) {
	return s.RegisterHubFromIP(ctx, req, "")
}

func (s *Service) RegisterHubFromIP(ctx context.Context, req RegisterHubRequest, clientIP string) (*RegisterHubResult, error) {
	ownerEmail := normalizeEmail(req.OwnerEmail)
	if err := s.checkEmailAllowed(ctx, ownerEmail); err != nil {
		return nil, err
	}
	if err := s.checkIPAllowed(ctx, clientIP); err != nil {
		return nil, err
	}

	now := time.Now()
	rawSecret, err := randomToken()
	if err != nil {
		return nil, err
	}

	capJSON, err := json.Marshal(req.Capabilities)
	if err != nil {
		return nil, err
	}

	installationID := strings.TrimSpace(req.InstallationID)
	if installationID != "" {
		existing, err := s.hubs.GetByInstallationID(ctx, installationID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			existing.OwnerEmail = ownerEmail
			existing.Name = strings.TrimSpace(req.Name)
			existing.Description = strings.TrimSpace(req.Description)
			existing.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
			existing.Host = strings.TrimSpace(req.Host)
			existing.Port = req.Port
			existing.Visibility = normalizeVisibility(req.Visibility)
			existing.EnrollmentMode = defaultIfEmpty(strings.TrimSpace(req.EnrollmentMode), "open")
			existing.CapabilitiesJSON = string(capJSON)
			existing.HubSecretHash = hashToken(rawSecret)
			existing.LastSeenAt = &now
			existing.UpdatedAt = now
			if existing.IsDisabled {
				existing.Status = "disabled"
			} else {
				existing.Status = "online"
			}

			if err := s.hubs.UpdateRegistration(ctx, existing); err != nil {
				return nil, err
			}
			if s.links != nil && existing.OwnerEmail != "" {
				_ = s.links.DeleteByHubID(ctx, existing.ID)
				_ = s.links.Create(ctx, &store.HubUserLink{
					ID:        newID("hul"),
					HubID:     existing.ID,
					Email:     existing.OwnerEmail,
					IsDefault: true,
					CreatedAt: now,
					UpdatedAt: now,
				})
			}

			return &RegisterHubResult{
				HubID:     existing.ID,
				HubSecret: rawSecret,
			}, nil
		}
	}

	hub := &store.HubInstance{
		ID:               newID("hub"),
		InstallationID:   installationID,
		OwnerEmail:       ownerEmail,
		Name:             strings.TrimSpace(req.Name),
		Description:      strings.TrimSpace(req.Description),
		BaseURL:          strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"),
		Host:             strings.TrimSpace(req.Host),
		Port:             req.Port,
		Visibility:       normalizeVisibility(req.Visibility),
		EnrollmentMode:   defaultIfEmpty(strings.TrimSpace(req.EnrollmentMode), "open"),
		Status:           "online",
		IsDisabled:       false,
		DisabledReason:   "",
		CapabilitiesJSON: string(capJSON),
		HubSecretHash:    hashToken(rawSecret),
		LastSeenAt:       &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.hubs.Create(ctx, hub); err != nil {
		return nil, err
	}
	if s.links != nil && hub.OwnerEmail != "" {
		_ = s.links.Create(ctx, &store.HubUserLink{
			ID:        newID("hul"),
			HubID:     hub.ID,
			Email:     hub.OwnerEmail,
			IsDefault: true,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	return &RegisterHubResult{
		HubID:     hub.ID,
		HubSecret: rawSecret,
	}, nil
}

func (s *Service) HeartbeatHub(ctx context.Context, hubID string) error {
	return s.HeartbeatHubWithSecret(ctx, hubID, "")
}

func (s *Service) HeartbeatHubWithSecret(ctx context.Context, hubID, rawSecret string) error {
	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil {
		return err
	}
	if hub == nil {
		return ErrHubUnauthorized
	}
	if rawSecret != "" && hub.HubSecretHash != hashToken(rawSecret) {
		return ErrHubUnauthorized
	}
	return s.hubs.UpdateHeartbeat(ctx, hubID, time.Now())
}

func (s *Service) ListHubs(ctx context.Context) ([]*store.HubInstance, error) {
	return s.hubs.ListAll(ctx)
}

func (s *Service) UpdateVisibility(ctx context.Context, hubID, visibility string) error {
	if strings.TrimSpace(hubID) == "" {
		return errors.New("hub id is required")
	}
	return s.hubs.UpdateVisibility(ctx, strings.TrimSpace(hubID), normalizeVisibility(visibility), time.Now())
}

func (s *Service) DisableHub(ctx context.Context, hubID, reason string) error {
	return s.hubs.SetDisabled(ctx, hubID, true, strings.TrimSpace(reason), time.Now())
}

func (s *Service) EnableHub(ctx context.Context, hubID string) error {
	return s.hubs.SetDisabled(ctx, hubID, false, "", time.Now())
}

func (s *Service) DeleteHub(ctx context.Context, hubID string) error {
	if s.links != nil {
		if err := s.links.DeleteByHubID(ctx, hubID); err != nil {
			return err
		}
	}
	return s.hubs.DeleteByID(ctx, hubID)
}

func (s *Service) AddBlockedEmail(ctx context.Context, email, reason string) error {
	if s.blockedEmails == nil {
		return nil
	}
	now := time.Now()
	return s.blockedEmails.Create(ctx, &store.BlockedEmail{
		ID:        newID("be"),
		Email:     normalizeEmail(email),
		Reason:    strings.TrimSpace(reason),
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *Service) ListBlockedEmails(ctx context.Context) ([]*store.BlockedEmail, error) {
	if s.blockedEmails == nil {
		return nil, nil
	}
	return s.blockedEmails.List(ctx)
}

func (s *Service) RemoveBlockedEmail(ctx context.Context, email string) error {
	if s.blockedEmails == nil {
		return nil
	}
	return s.blockedEmails.DeleteByEmail(ctx, normalizeEmail(email))
}

func (s *Service) AddBlockedIP(ctx context.Context, ip, reason string) error {
	if s.blockedIPs == nil {
		return nil
	}
	now := time.Now()
	return s.blockedIPs.Create(ctx, &store.BlockedIP{
		ID:        newID("bi"),
		IP:        strings.TrimSpace(ip),
		Reason:    strings.TrimSpace(reason),
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *Service) ListBlockedIPs(ctx context.Context) ([]*store.BlockedIP, error) {
	if s.blockedIPs == nil {
		return nil, nil
	}
	return s.blockedIPs.List(ctx)
}

func (s *Service) RemoveBlockedIP(ctx context.Context, ip string) error {
	if s.blockedIPs == nil {
		return nil
	}
	return s.blockedIPs.DeleteByIP(ctx, strings.TrimSpace(ip))
}

func (s *Service) checkEmailAllowed(ctx context.Context, email string) error {
	if s.blockedEmails == nil || email == "" {
		return nil
	}

	blocked, err := s.blockedEmails.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	if blocked != nil {
		return ErrEmailBlocked
	}
	return nil
}

func (s *Service) checkIPAllowed(ctx context.Context, ip string) error {
	if s.blockedIPs == nil {
		return nil
	}

	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}

	blocked, err := s.blockedIPs.GetByIP(ctx, ip)
	if err != nil {
		return err
	}
	if blocked != nil {
		return ErrIPBlocked
	}
	return nil
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func defaultIfEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func normalizeVisibility(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "shared":
		return "shared"
	case "public":
		return "public"
	default:
		return "private"
	}
}

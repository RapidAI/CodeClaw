package httpapi

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
)

// SkillMarketHandlers 处理 SkillMarket 相关的 HTTP 请求。
type SkillMarketHandlers struct {
	store      *skillmarket.Store
	userSvc    *skillmarket.UserService
	creditsSvc *skillmarket.CreditsService
	processor  *skillmarket.Processor
	rsaPrivKey *rsa.PrivateKey
	pendingDir string
	dataDir    string
}

// SkillMarketConfig 是创建 SkillMarketHandlers 所需的配置。
type SkillMarketConfig struct {
	Store      *skillmarket.Store
	UserSvc    *skillmarket.UserService
	CreditsSvc *skillmarket.CreditsService
	Processor  *skillmarket.Processor
	RSAPrivKey *rsa.PrivateKey
	PendingDir string
	DataDir    string
}

// NewSkillMarketHandlers 创建 SkillMarket HTTP handlers。
func NewSkillMarketHandlers(cfg SkillMarketConfig) *SkillMarketHandlers {
	return &SkillMarketHandlers{
		store:      cfg.Store,
		userSvc:    cfg.UserSvc,
		creditsSvc: cfg.CreditsSvc,
		processor:  cfg.Processor,
		rsaPrivKey: cfg.RSAPrivKey,
		pendingDir: cfg.PendingDir,
		dataDir:    cfg.DataDir,
	}
}

// ── Upload Submit ───────────────────────────────────────────────────────

// SubmitSkill handles POST /api/v1/skills/submit (multipart/form-data).
func (h *SkillMarketHandlers) SubmitSkill(w http.ResponseWriter, r *http.Request) {
	// 限制上传大小 100MB
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		smError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}

	file, header, err := r.FormFile("zip")
	if err != nil {
		smError(w, http.StatusBadRequest, "zip file is required")
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		smError(w, http.StatusBadRequest, "file must be a .zip")
		return
	}

	// 确保用户账户存在
	user, err := h.userSvc.EnsureAccount(r.Context(), email)
	if err != nil {
		smError(w, http.StatusInternalServerError, "ensure account: "+err.Error())
		return
	}

	// 保存 zip 到 pending 目录
	subID := fmt.Sprintf("sub-%d", uniqueCounter())
	_ = os.MkdirAll(h.pendingDir, 0o755)
	zipPath := filepath.Join(h.pendingDir, subID+".zip")
	out, err := os.Create(zipPath)
	if err != nil {
		smError(w, http.StatusInternalServerError, "save zip: "+err.Error())
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		smError(w, http.StatusInternalServerError, "save zip: "+err.Error())
		return
	}
	out.Close()

	// 创建 submission 记录
	sub := &skillmarket.SkillSubmission{
		ID:     subID,
		Email:  email,
		UserID: user.ID,
		Status: "pending",
		ZipPath: zipPath,
	}
	now := time.Now()
	sub.CreatedAt = now
	sub.UpdatedAt = now
	if err := h.store.CreateSubmission(r.Context(), sub); err != nil {
		smError(w, http.StatusInternalServerError, "create submission: "+err.Error())
		return
	}

	// 入队异步处理
	h.processor.Enqueue(subID)

	writeJSON(w, http.StatusOK, map[string]any{
		"submission_id": subID,
		"status":        "pending",
	})
}

// GetSubmissionStatus handles GET /api/v1/skills/submissions/{id}.
func (h *SkillMarketHandlers) GetSubmissionStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		smError(w, http.StatusBadRequest, "submission id is required")
		return
	}
	sub, err := h.store.GetSubmissionByID(r.Context(), id)
	if err != nil {
		smError(w, http.StatusNotFound, "submission not found")
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// ── Account ─────────────────────────────────────────────────────────────

// EnsureAccount handles POST /api/v1/account/ensure.
func (h *SkillMarketHandlers) EnsureAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}
	user, err := h.userSvc.EnsureAccount(r.Context(), strings.TrimSpace(req.Email))
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// GetAccount handles GET /api/v1/account/{email}.
func (h *SkillMarketHandlers) GetAccount(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if email == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}
	user, err := h.userSvc.GetAccount(r.Context(), email)
	if err != nil {
		smError(w, http.StatusNotFound, "account not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// VerifyAccount handles POST /api/v1/account/verify.
func (h *SkillMarketHandlers) VerifyAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email  string `json:"email"`
		Method string `json:"method"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Email == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}
	user, err := h.userSvc.VerifyAccount(r.Context(), req.Email, req.Method)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// ── Credits ─────────────────────────────────────────────────────────────

// GetCreditsBalance handles GET /api/v1/credits/balance.
func (h *SkillMarketHandlers) GetCreditsBalance(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		smError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	balance, err := h.creditsSvc.GetBalance(r.Context(), userID)
	if err != nil {
		smError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"balance": balance})
}

// GetCreditsTransactions handles GET /api/v1/credits/transactions.
func (h *SkillMarketHandlers) GetCreditsTransactions(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		smError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	txs, total, err := h.store.ListTransactionsByUser(r.Context(), userID, offset, limit)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": txs, "total": total})
}

// TopUpCredits handles POST /api/v1/credits/topup.
func (h *SkillMarketHandlers) TopUpCredits(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Amount int64  `json:"amount"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.creditsSvc.TopUp(r.Context(), req.UserID, req.Amount); err != nil {
		status := http.StatusInternalServerError
		if err == skillmarket.ErrUnverifiedAccount {
			status = http.StatusForbidden
		}
		smError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// WithdrawCredits handles POST /api/v1/credits/withdraw.
func (h *SkillMarketHandlers) WithdrawCredits(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Amount int64  `json:"amount"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.creditsSvc.Withdraw(r.Context(), req.UserID, req.Amount); err != nil {
		status := http.StatusInternalServerError
		if err == skillmarket.ErrUnverifiedAccount {
			status = http.StatusForbidden
		} else if err == skillmarket.ErrInsufficientCredits {
			status = http.StatusPaymentRequired
		}
		smError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Crypto ──────────────────────────────────────────────────────────────

// GetPublicKey handles GET /api/v1/crypto/pubkey.
func (h *SkillMarketHandlers) GetPublicKey(w http.ResponseWriter, r *http.Request) {
	pemData, err := skillmarket.LoadPublicKeyPEM(h.dataDir)
	if err != nil {
		smError(w, http.StatusInternalServerError, "public key not available")
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.WriteHeader(http.StatusOK)
	w.Write(pemData)
}

// ── helpers ─────────────────────────────────────────────────────────────

func smError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

var _counter uint64

func uniqueCounter() uint64 {
	_counter++
	return _counter
}

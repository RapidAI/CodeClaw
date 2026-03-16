package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const openclawIMConfigKey = "openclaw_im_config"

type OpenclawIMConfigState struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret"`
}

func GetOpenclawIMConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := system.Get(r.Context(), openclawIMConfigKey)
		if err != nil || raw == "" {
			writeJSON(w, http.StatusOK, OpenclawIMConfigState{})
			return
		}
		var cfg OpenclawIMConfigState
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			writeJSON(w, http.StatusOK, OpenclawIMConfigState{})
			return
		}
		if cfg.Secret != "" {
			cfg.Secret = maskSecret(cfg.Secret)
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func UpdateOpenclawIMConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg OpenclawIMConfigState
		if err := json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if cfg.WebhookURL != "" {
			u, err := url.Parse(cfg.WebhookURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				writeError(w, http.StatusBadRequest, "INVALID_WEBHOOK_URL", "Webhook URL must be a valid HTTP(S) URL")
				return
			}
		}
		if isMasked(cfg.Secret) {
			old := loadOpenclawIMConfig(r, system)
			cfg.Secret = old.Secret
		}
		data, _ := json.Marshal(cfg)
		if err := system.Set(r.Context(), openclawIMConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "OPENCLAW_IM_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		resp := cfg
		if resp.Secret != "" {
			resp.Secret = maskSecret(resp.Secret)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func loadOpenclawIMConfig(r *http.Request, system store.SystemSettingsRepository) OpenclawIMConfigState {
	raw, err := system.Get(r.Context(), openclawIMConfigKey)
	if err != nil || raw == "" {
		return OpenclawIMConfigState{}
	}
	var cfg OpenclawIMConfigState
	_ = json.Unmarshal([]byte(raw), &cfg)
	return cfg
}

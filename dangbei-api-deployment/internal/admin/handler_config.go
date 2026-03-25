package admin

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
)

func (h *Handler) getConfig(w http.ResponseWriter, _ *http.Request) {
	snap := h.Store.Snapshot()
	safe := map[string]any{
		"keys":     snap.Keys,
		"accounts": []map[string]any{},
		"claude_mapping": func() map[string]string {
			if len(snap.ClaudeMapping) > 0 {
				return snap.ClaudeMapping
			}
			return snap.ClaudeModelMap
		}(),
	}
	accounts := make([]map[string]any, 0, len(snap.Accounts))
	for _, acc := range snap.Accounts {
		token := strings.TrimSpace(acc.Token)
		preview := ""
		if token != "" {
			if len(token) > 20 {
				preview = token[:20] + "..."
			} else {
				preview = token
			}
		}
		accounts = append(accounts, map[string]any{
			"identifier":    acc.Identifier(),
			"email":         acc.Email,
			"mobile":        acc.Mobile,
			"has_password":  strings.TrimSpace(acc.Password) != "",
			"has_token":     token != "",
			"token_preview": preview,
		})
	}
	safe["accounts"] = accounts
	writeJSON(w, http.StatusOK, safe)
}

func (h *Handler) updateConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	old := h.Store.Snapshot()
	err := h.Store.Update(func(c *config.Config) error {
		if keys, ok := toStringSlice(req["keys"]); ok {
			c.Keys = keys
		}
		if accountsRaw, ok := req["accounts"].([]any); ok {
			existing := map[string]config.Account{}
			for _, a := range old.Accounts {
				existing[a.Identifier()] = a
			}
			accounts := make([]config.Account, 0, len(accountsRaw))
			for _, item := range accountsRaw {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				acc := toAccount(m)
				id := acc.Identifier()
				if prev, ok := existing[id]; ok {
					if strings.TrimSpace(acc.Password) == "" {
						acc.Password = prev.Password
					}
					if strings.TrimSpace(acc.Token) == "" {
						acc.Token = prev.Token
					}
				}
				accounts = append(accounts, acc)
			}
			c.Accounts = accounts
		}
		if m, ok := req["claude_mapping"].(map[string]any); ok {
			newMap := map[string]string{}
			for k, v := range m {
				newMap[k] = fmt.Sprintf("%v", v)
			}
			c.ClaudeMapping = newMap
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "配置已更新"})
}

func (h *Handler) addKey(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	key, _ := req["key"].(string)
	key = strings.TrimSpace(key)
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Key 不能为空"})
		return
	}
	err := h.Store.Update(func(c *config.Config) error {
		for _, k := range c.Keys {
			if k == key {
				return fmt.Errorf("Key 已存在")
			}
		}
		c.Keys = append(c.Keys, key)
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "total_keys": len(h.Store.Snapshot().Keys)})
}

func (h *Handler) deleteKey(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	err := h.Store.Update(func(c *config.Config) error {
		idx := -1
		for i, k := range c.Keys {
			if k == key {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("Key 不存在")
		}
		c.Keys = append(c.Keys[:idx], c.Keys[idx+1:]...)
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "total_keys": len(h.Store.Snapshot().Keys)})
}

func (h *Handler) batchImport(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "无效的 JSON 格式"})
		return
	}
	importedKeys, importedAccounts := 0, 0
	err := h.Store.Update(func(c *config.Config) error {
		if keys, ok := req["keys"].([]any); ok {
			existing := map[string]bool{}
			for _, k := range c.Keys {
				existing[k] = true
			}
			for _, k := range keys {
				key := strings.TrimSpace(fmt.Sprintf("%v", k))
				if key == "" || existing[key] {
					continue
				}
				c.Keys = append(c.Keys, key)
				existing[key] = true
				importedKeys++
			}
		}
		if accounts, ok := req["accounts"].([]any); ok {
			existing := map[string]bool{}
			for _, a := range c.Accounts {
				existing[a.Identifier()] = true
			}
			for _, item := range accounts {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				acc := toAccount(m)
				id := acc.Identifier()
				if id == "" || existing[id] {
					continue
				}
				c.Accounts = append(c.Accounts, acc)
				existing[id] = true
				importedAccounts++
			}
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "imported_keys": importedKeys, "imported_accounts": importedAccounts})
}

func (h *Handler) exportConfig(w http.ResponseWriter, _ *http.Request) {
	jsonStr, b64, err := h.Store.ExportJSONAndBase64()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"json": jsonStr, "base64": b64})
}

func (h *Handler) computeSyncHash() string {
	snap := h.Store.Snapshot()
	syncable := map[string]any{"keys": snap.Keys, "accounts": []map[string]any{}}
	accounts := make([]map[string]any, 0, len(snap.Accounts))
	for _, a := range snap.Accounts {
		m := map[string]any{}
		if a.Email != "" {
			m["email"] = a.Email
		}
		if a.Mobile != "" {
			m["mobile"] = a.Mobile
		}
		if a.Password != "" {
			m["password"] = a.Password
		}
		accounts = append(accounts, m)
	}
	sort.Slice(accounts, func(i, j int) bool {
		ai := fmt.Sprintf("%v%v", accounts[i]["email"], accounts[i]["mobile"])
		aj := fmt.Sprintf("%v%v", accounts[j]["email"], accounts[j]["mobile"])
		return ai < aj
	})
	syncable["accounts"] = accounts
	b, _ := json.Marshal(syncable)
	sum := md5.Sum(b)
	return fmt.Sprintf("%x", sum)
}

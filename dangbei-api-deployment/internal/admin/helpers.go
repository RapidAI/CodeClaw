package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"ds2api/internal/config"
	"ds2api/internal/util"
)

// writeJSON and intFrom are package-internal aliases for the shared util versions.
var writeJSON = util.WriteJSON
var intFrom = util.IntFrom

func reverseAccounts(a []config.Account) {
	for i, j := 0, len(a)-1; i < j; i, j = i+1, j-1 {
		a[i], a[j] = a[j], a[i]
	}
}

func intFromQuery(r *http.Request, key string, d int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return d
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return d
	}
	return n
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilIfZero(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func toStringSlice(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		out = append(out, strings.TrimSpace(fmt.Sprintf("%v", item)))
	}
	return out, true
}

func toAccount(m map[string]any) config.Account {
	return config.Account{
		Email:    fieldString(m, "email"),
		Mobile:   fieldString(m, "mobile"),
		Password: fieldString(m, "password"),
		Token:    fieldString(m, "token"),
	}
}

func fieldString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func statusOr(v int, d int) int {
	if v == 0 {
		return d
	}
	return v
}

func accountMatchesIdentifier(acc config.Account, identifier string) bool {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return false
	}
	if strings.TrimSpace(acc.Email) == id {
		return true
	}
	if strings.TrimSpace(acc.Mobile) == id {
		return true
	}
	return acc.Identifier() == id
}

func findAccountByIdentifier(store *config.Store, identifier string) (config.Account, bool) {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return config.Account{}, false
	}
	if acc, ok := store.FindAccount(id); ok {
		return acc, true
	}
	accounts := store.Snapshot().Accounts
	for _, acc := range accounts {
		if accountMatchesIdentifier(acc, id) {
			return acc, true
		}
	}
	return config.Account{}, false
}

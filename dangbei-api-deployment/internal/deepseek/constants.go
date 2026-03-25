package deepseek

const (
	DeepSeekHost             = "chat.deepseek.com"
	DeepSeekLoginURL         = "https://chat.deepseek.com/api/v0/users/login"
	DeepSeekCreateSessionURL = "https://chat.deepseek.com/api/v0/chat_session/create"
	DeepSeekCreatePowURL     = "https://chat.deepseek.com/api/v0/chat/create_pow_challenge"
	DeepSeekCompletionURL    = "https://chat.deepseek.com/api/v0/chat/completion"
)

var BaseHeaders = map[string]string{
	"Host":              "chat.deepseek.com",
	"User-Agent":        "DeepSeek/1.6.11 Android/35",
	"Accept":            "application/json",
	"Content-Type":      "application/json",
	"x-client-platform": "android",
	"x-client-version":  "1.6.11",
	"x-client-locale":   "zh_CN",
	"accept-charset":    "UTF-8",
}

const (
	KeepAliveTimeout  = 5
	StreamIdleTimeout = 30
	MaxKeepaliveCount = 10
)

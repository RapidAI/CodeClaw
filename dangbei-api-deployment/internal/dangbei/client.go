package dangbei

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DangbeiAPIURL = "https://ai-api.dangbei.net/ai-search/chatApi/v2/chat"
	AppType       = "6"
	AppVersion    = "1.3.8"
	ClientVer     = "1.0.2"
	Lang          = "zh"
)

type Client struct {
	httpClient *http.Client
	token      string
}

type ChatRequest struct {
	Stream           bool                   `json:"stream"`
	BotCode          string                 `json:"botCode"`
	ConversationID   string                 `json:"conversationId"`
	Question         string                 `json:"question"`
	Model            string                 `json:"model"`
	ChatOption       map[string]interface{} `json:"chatOption"`
	KnowledgeList    []interface{}          `json:"knowledgeList"`
	AnonymousKey     string                 `json:"anonymousKey"`
	UUID             string                 `json:"uuid"`
	ChatID           string                 `json:"chatId"`
	Files            []interface{}          `json:"files"`
	Reference        []interface{}          `json:"reference"`
	Role             string                 `json:"role"`
	Status           string                 `json:"status"`
	Content          string                 `json:"content"`
	UserAction       string                 `json:"userAction"`
	AgentID          string                 `json:"agentId"`
}

type SSEMessage struct {
	Role            string `json:"role"`
	Type            string `json:"type"`
	Content         string `json:"content"`
	ContentType     string `json:"content_type"`
	ID              string `json:"id"`
	ParentMsgID     string `json:"parentMsgId"`
	ConversationID  string `json:"conversation_id"`
	CreatedAt       int64  `json:"created_at"`
	RequestID       string `json:"requestId"`
	SupportDownload bool   `json:"supportDownload"`
}

func NewClient(token string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 0, // No timeout for streaming
		},
		token: token,
	}
}

// generateNonce generates a random nonce string
func generateNonce() string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 21)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// generateSign generates a dummy signature
// Testing shows the API doesn't validate the sign, only token matters
func generateSign(timestamp, nonce, token string) string {
	// Simple MD5 of concatenated values
	data := nonce + timestamp + token
	hash := md5.Sum([]byte(data))
	return strings.ToUpper(hex.EncodeToString(hash[:]))
}

func (c *Client) Chat(ctx context.Context, req *ChatRequest) (<-chan SSEMessage, <-chan error) {
	msgChan := make(chan SSEMessage, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(msgChan)
		defer close(errChan)

		// Prepare request body
		bodyBytes, err := json.Marshal(req)
		if err != nil {
			errChan <- fmt.Errorf("marshal request: %w", err)
			return
		}

		// Generate auth parameters
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := generateNonce()
		sign := generateSign(timestamp, nonce, c.token)

		// Create HTTP request
		httpReq, err := http.NewRequestWithContext(ctx, "POST", DangbeiAPIURL, bytes.NewReader(bodyBytes))
		if err != nil {
			errChan <- fmt.Errorf("create request: %w", err)
			return
		}

		// Set headers
		httpReq.Header.Set("Accept", "*/*")
		httpReq.Header.Set("Origin", "https://ai.dangbei.com")
		httpReq.Header.Set("Referer", "https://ai.dangbei.com/")
		httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		httpReq.Header.Set("appType", AppType)
		httpReq.Header.Set("appVersion", AppVersion)
		httpReq.Header.Set("client-ver", ClientVer)
		httpReq.Header.Set("content-type", "application/json")
		httpReq.Header.Set("lang", Lang)
		httpReq.Header.Set("nonce", nonce)
		httpReq.Header.Set("sign", sign)
		httpReq.Header.Set("timestamp", timestamp)
		httpReq.Header.Set("token", c.token)

		// Send request
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			errChan <- fmt.Errorf("send request: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errChan <- fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
			return
		}

		// Parse SSE stream
		scanner := bufio.NewScanner(resp.Body)
		var currentEvent string

		for scanner.Scan() {
			line := scanner.Text()

			if strings.HasPrefix(line, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}

			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data == "" {
					continue
				}

				// Parse JSON data
				var msg SSEMessage
				if err := json.Unmarshal([]byte(data), &msg); err != nil {
					// Skip invalid JSON
					continue
				}

				// Only send delta messages
				if currentEvent == "conversation.message.delta" {
					select {
					case msgChan <- msg:
					case <-ctx.Done():
						return
					}
				}

				// Check for completion
				if currentEvent == "conversation.chat.completed" {
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("scan stream: %w", err)
		}
	}()

	return msgChan, errChan
}

package feishu

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/go-lark/lark/v2"
)

// FeishuPlugin implements the im.IMPlugin interface by composing the existing
// Notifier (outbound messaging, user resolution, card building) and
// WebhookHandler (inbound message reception) functionality.
//
// When an IM Adapter is wired (via SetAdapter), incoming bot messages are
// converted to im.IncomingMessage and routed through the adapter pipeline.
// When no adapter is set, the plugin falls back to the legacy handleCommand /
// handleSendInput behaviour for full backward compatibility.
type FeishuPlugin struct {
	notifier *Notifier

	// messageHandler is the callback registered by IM Adapter via ReceiveMessage.
	messageHandler func(msg im.IncomingMessage)

	// adapter is an optional reference to the IM Adapter. When set,
	// handleBotMessage routes messages through the adapter pipeline.
	adapter IMAdapter
}

// IMAdapter is a minimal interface for the IM Adapter so that the feishu
// package does not import hub/internal/im (avoiding circular deps if needed).
// In practice the *im.Adapter satisfies this interface.
type IMAdapter interface {
	HandleMessage(ctx context.Context, msg im.IncomingMessage)
}

// NewPlugin creates a FeishuPlugin wrapping the given Notifier.
func NewPlugin(n *Notifier) *FeishuPlugin {
	return &FeishuPlugin{
		notifier: n,
	}
}

// SetAdapter wires the IM Adapter for message routing. When set, incoming
// bot messages are converted to IncomingMessage and forwarded to the adapter
// instead of the legacy command handler.
func (p *FeishuPlugin) SetAdapter(a IMAdapter) {
	p.adapter = a
}

// ---------------------------------------------------------------------------
// im.IMPlugin interface implementation
// ---------------------------------------------------------------------------

// Name returns the plugin identifier.
func (p *FeishuPlugin) Name() string { return "feishu" }

// ReceiveMessage registers a callback that the IM Adapter uses to receive
// standardised incoming messages from this plugin.
func (p *FeishuPlugin) ReceiveMessage(handler func(msg im.IncomingMessage)) {
	p.messageHandler = handler
}

// SendText sends a plain text message to the target user via Feishu.
// Reuses the existing replyText logic.
func (p *FeishuPlugin) SendText(ctx context.Context, target im.UserTarget, text string) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required")
	}
	replyText(p.notifier, openID, text)
	return nil
}

// SendCard sends a rich card message to the target user via Feishu.
// Converts the OutgoingMessage to a Feishu interactive card using the
// existing buildCardJSON logic.
func (p *FeishuPlugin) SendCard(ctx context.Context, target im.UserTarget, card im.OutgoingMessage) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required")
	}

	// Convert OutgoingMessage fields to cardField slice for buildCardJSON.
	var fields []cardField
	if card.StatusIcon != "" || card.StatusCode != 0 {
		// Include status as a field if present.
		statusText := card.StatusIcon
		if card.StatusCode != 0 {
			statusText = fmt.Sprintf("%s %d", card.StatusIcon, card.StatusCode)
		}
		if statusText != "" {
			fields = append(fields, cardField{label: "状态", value: statusText})
		}
	}
	if card.Body != "" {
		fields = append(fields, cardField{label: "详情", value: card.Body})
	}
	for _, f := range card.Fields {
		fields = append(fields, cardField{label: f.Label, value: f.Value})
	}
	// Append action buttons as text hints (Feishu cards support markdown).
	for _, a := range card.Actions {
		hint := a.Label
		if a.Command != "" {
			hint = fmt.Sprintf("%s → `%s`", a.Label, a.Command)
		}
		fields = append(fields, cardField{label: "操作", value: hint})
	}

	title := card.Title
	if title == "" {
		title = "消息"
	}
	color := statusColorFromCode(card.StatusCode)
	cardJSON := buildCardJSON(title, color, fields)

	p.notifier.sendToUserByOpenID(openID, cardJSON)
	return nil
}

// SendImage sends an image message to the target user via Feishu.
// If imageKey looks like base64-encoded PNG data (not a Feishu image_key),
// it is decoded, uploaded to Feishu, and then sent.
func (p *FeishuPlugin) SendImage(ctx context.Context, target im.UserTarget, imageKey string, caption string) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required")
	}

	// Detect base64 PNG data vs Feishu image_key.
	// Feishu image_keys look like "img_v2_xxx" or "img_xxx".
	if !strings.HasPrefix(imageKey, "img_") && len(imageKey) > 200 {
		// Likely base64 image data — decode and upload to Feishu.
		uploaded, err := p.uploadBase64Image(ctx, imageKey)
		if err != nil {
			return fmt.Errorf("feishu: upload image failed: %w", err)
		}
		imageKey = uploaded
	}

	sendImagePost(p.notifier, openID, imageKey, caption)
	return nil
}

// uploadBase64Image decodes base64 PNG data and uploads it to Feishu,
// returning the Feishu image_key.
func (p *FeishuPlugin) uploadBase64Image(ctx context.Context, b64Data string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("png decode: %w", err)
	}

	bot := p.notifier.Bot()
	if bot == nil {
		return "", fmt.Errorf("feishu bot not initialized")
	}

	// Use a dedicated context with generous timeout for image upload.
	uploadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := bot.UploadImageObject(uploadCtx, img)
	if err != nil {
		return "", fmt.Errorf("upload to feishu: %w", err)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("feishu API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return resp.Data.ImageKey, nil
}

// SendFile sends a file to the target user via Feishu.
// It uploads the base64-encoded file data using the Feishu file upload API,
// then sends it as a file message.
func (p *FeishuPlugin) SendFile(ctx context.Context, target im.UserTarget, fileData, fileName, mimeType string) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required")
	}

	bot := p.notifier.Bot()
	if bot == nil {
		return fmt.Errorf("feishu bot not initialized")
	}

	raw, err := base64.StdEncoding.DecodeString(fileData)
	if err != nil {
		return fmt.Errorf("feishu: base64 decode: %w", err)
	}

	// Determine file type for Feishu API.
	fileType := "stream"
	if strings.HasPrefix(mimeType, "image/") {
		// For images, use SendImage path instead.
		return p.SendImage(ctx, target, fileData, fileName)
	}

	// Use a dedicated context with generous timeout for file upload,
	// since the caller's context may have a shorter deadline that's
	// insufficient for large file uploads to Feishu.
	uploadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Upload file to Feishu.
	uploadResp, err := bot.UploadFile(uploadCtx, lark.UploadFileRequest{
		FileType: fileType,
		FileName: fileName,
		Reader:   bytes.NewReader(raw),
	})
	if err != nil {
		return fmt.Errorf("feishu: upload file: %w", err)
	}
	if uploadResp.Code != 0 {
		return fmt.Errorf("feishu: upload file API error: code=%d msg=%s", uploadResp.Code, uploadResp.Msg)
	}

	fileKey := uploadResp.Data.FileKey

	// Send file message.
	msg := lark.NewMsgBuffer(lark.MsgFile).
		BindOpenID(openID).
		File(fileKey).
		Build()

	if _, err := bot.PostMessage(ctx, msg); err != nil {
		return fmt.Errorf("feishu: send file message: %w", err)
	}

	return nil
}

// ResolveUser maps a Feishu open_id to the unified internal user ID.
// Reuses the existing resolveUserID logic (open_id → email → userID).
func (p *FeishuPlugin) ResolveUser(ctx context.Context, platformUID string) (string, error) {
	userID := p.notifier.resolveUserID(platformUID)
	if userID == "" {
		return "", fmt.Errorf("feishu: cannot resolve user for open_id %s (not bound)", platformUID)
	}
	return userID, nil
}

// LookupByEmail returns the Feishu open_id bound to the given email, or "".
// Implements im.BindingLookup for cross-IM verification.
func (p *FeishuPlugin) LookupByEmail(email string) string {
	return p.notifier.resolveOpenID(email)
}

// Capabilities declares that Feishu supports rich text cards, Markdown,
// images, and button interactions.
func (p *FeishuPlugin) Capabilities() im.CapabilityDeclaration {
	return im.CapabilityDeclaration{
		SupportsRichCard:    true,
		SupportsMarkdown:    true,
		SupportsImage:       true,
		SupportsFile:        true,
		SupportsButton:      true,
		SupportsMessageEdit: false,
		MaxTextLength:       4000,
	}
}

// Start is a no-op for Feishu — the webhook HTTP handler is registered
// externally and the lark.Bot is initialised by the Notifier constructor.
func (p *FeishuPlugin) Start(ctx context.Context) error {
	log.Printf("[feishu/plugin] started")
	return nil
}

// Stop is a no-op for Feishu — there are no persistent connections to tear down.
func (p *FeishuPlugin) Stop(ctx context.Context) error {
	log.Printf("[feishu/plugin] stopped")
	return nil
}

// ---------------------------------------------------------------------------
// Incoming message dispatch — bridge between webhook and IM Adapter
// ---------------------------------------------------------------------------

// DispatchBotMessage is called by the webhook handler (handleBotMessage) to
// route an incoming Feishu bot message. If the IM Adapter is wired, the
// message is converted to im.IncomingMessage and forwarded through the
// adapter pipeline. Otherwise, the legacy command/send-input flow is used.
//
// Returns true if the message was dispatched to the IM Adapter, false if
// the legacy path should handle it.
func (p *FeishuPlugin) DispatchBotMessage(openID, messageType, text string, raw json.RawMessage) bool {
	if p.adapter == nil && p.messageHandler == nil {
		return false // no adapter wired — use legacy path
	}

	msg := im.IncomingMessage{
		PlatformName: "feishu",
		PlatformUID:  openID,
		MessageType:  messageType,
		Text:         text,
		RawPayload:   raw,
		Timestamp:    time.Now(),
	}

	// If a message handler is registered (by IM Adapter's RegisterPlugin),
	// use it. This is the standard path.
	if p.messageHandler != nil {
		p.messageHandler(msg)
		return true
	}

	// Fallback: call adapter directly.
	if p.adapter != nil {
		p.adapter.HandleMessage(context.Background(), msg)
		return true
	}

	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sendToUserByOpenID sends a Feishu interactive card to a user identified by
// open_id. This is a thin wrapper around the lark bot API.
func (n *Notifier) sendToUserByOpenID(openID, cardJSON string) {
	if n == nil || n.bot == nil || openID == "" {
		return
	}
	msg := newCardMessage(openID, cardJSON)
	ctx := context.Background()
	resp, err := n.bot.PostMessage(ctx, msg)
	if err != nil {
		log.Printf("[feishu/plugin] send card failed (open_id=%s): %v", openID, err)
		return
	}
	if resp != nil && resp.Code != 0 {
		log.Printf("[feishu/plugin] API error (open_id=%s): code=%d msg=%s", openID, resp.Code, resp.Msg)
	}
}

// newCardMessage builds a lark OutcomingMessage for an interactive card
// addressed to the given open_id.
func newCardMessage(openID, cardJSON string) lark.OutcomingMessage {
	return lark.NewMsgBuffer(lark.MsgInteractive).
		BindOpenID(openID).
		Card(cardJSON).
		Build()
}

// statusColorFromCode maps an HTTP-style status code to a Feishu card
// header template colour.
func statusColorFromCode(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "green"
	case code >= 400 && code < 500:
		return "orange"
	case code >= 500:
		return "red"
	default:
		return "blue"
	}
}

// isLegacyCommand returns true if the text starts with "/" — these should
// still be handled by the legacy handleCommand path even when the IM Adapter
// is active, to preserve full backward compatibility for slash commands.
func isLegacyCommand(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "/")
}

package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
	offset     int64
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Date      int64  `json:"date"`
	Chat      Chat   `json:"chat"`
	From      *User  `json:"from"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

func NewClient(token string, timeout time.Duration) *Client {
	return &Client{
		token:   token,
		baseURL: "https://api.telegram.org",
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) GetMe(ctx context.Context) error {
	var response struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := c.postJSON(ctx, "getMe", map[string]any{}, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram getMe failed: %s", response.Description)
	}
	return nil
}

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

func (c *Client) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	payload := map[string]any{
		"commands": commands,
	}
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := c.postJSON(ctx, "setMyCommands", payload, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram setMyCommands failed: %s", response.Description)
	}
	return nil
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) (int64, error) {
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	return c.sendMessagePayload(ctx, payload)
}

func (c *Client) SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, keyboard InlineKeyboardMarkup) (int64, error) {
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
		"reply_markup":             keyboard,
	}
	return c.sendMessagePayload(ctx, payload)
}

func (c *Client) PinChatMessage(ctx context.Context, chatID int64, messageID int64) error {
	payload := map[string]any{
		"chat_id":              chatID,
		"message_id":           messageID,
		"disable_notification": true,
	}
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := c.postJSON(ctx, "pinChatMessage", payload, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram pinChatMessage failed: %s", response.Description)
	}
	return nil
}

func (c *Client) UnpinChatMessage(ctx context.Context, chatID int64, messageID int64) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := c.postJSON(ctx, "unpinChatMessage", payload, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram unpinChatMessage failed: %s", response.Description)
	}
	return nil
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackQueryID string, text string) error {
	payload := map[string]any{
		"callback_query_id": callbackQueryID,
	}
	if strings.TrimSpace(text) != "" {
		payload["text"] = text
	}
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := c.postJSON(ctx, "answerCallbackQuery", payload, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram answerCallbackQuery failed: %s", response.Description)
	}
	return nil
}

func (c *Client) sendMessagePayload(ctx context.Context, payload map[string]any) (int64, error) {
	var response struct {
		OK          bool    `json:"ok"`
		Description string  `json:"description"`
		Result      Message `json:"result"`
	}
	if err := c.postJSON(ctx, "sendMessage", payload, &response); err != nil {
		return 0, err
	}
	if !response.OK {
		return 0, fmt.Errorf("telegram sendMessage failed: %s", response.Description)
	}
	return response.Result.MessageID, nil
}

func (c *Client) Poll(ctx context.Context, handler func(context.Context, Update)) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, err := c.getUpdates(ctx)
		if err != nil {
			log.Printf("telegram polling error: %s", c.redact(err.Error()))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
				continue
			}
		}
		for _, update := range updates {
			if update.UpdateID >= c.offset {
				c.offset = update.UpdateID + 1
			}
			handler(ctx, update)
		}
	}
}

func (c *Client) getUpdates(ctx context.Context) ([]Update, error) {
	endpoint := c.apiURL("getUpdates")
	values := url.Values{}
	values.Set("timeout", fmt.Sprintf("%d", c.pollTimeoutSeconds()))
	values.Set("offset", fmt.Sprintf("%d", c.offset))
	values.Set("allowed_updates", `["message","callback_query"]`)
	endpoint.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram getUpdates http %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		OK          bool     `json:"ok"`
		Description string   `json:"description"`
		Result      []Update `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, fmt.Errorf("telegram getUpdates failed: %s", response.Description)
	}
	return response.Result, nil
}

func (c *Client) postJSON(ctx context.Context, method string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL(method).String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram %s http %d: %s", method, resp.StatusCode, string(respBody))
	}
	return json.Unmarshal(respBody, target)
}

func (c *Client) apiURL(method string) *url.URL {
	endpoint, _ := url.Parse(fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method))
	return endpoint
}

func (c *Client) redact(text string) string {
	if c.token == "" {
		return text
	}
	return strings.ReplaceAll(text, c.token, "<redacted-token>")
}

func (c *Client) pollTimeoutSeconds() int {
	if c.httpClient.Timeout <= 0 {
		return 45
	}
	timeout := int(c.httpClient.Timeout.Seconds()) - 2
	if timeout < 1 {
		return 1
	}
	if timeout > 45 {
		return 45
	}
	return timeout
}

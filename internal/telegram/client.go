package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token:   token,
		baseURL: "https://api.telegram.org",
		http: &http.Client{
			Timeout: 70 * time.Second,
		},
	}
}

func (c *Client) GetUpdates(ctx context.Context, offset int, timeout time.Duration) ([]Update, error) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	u, err := url.Parse(fmt.Sprintf("%s/bot%s/getUpdates", c.baseURL, c.token))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("timeout", strconv.Itoa(int(timeout.Seconds())))
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	q.Set("allowed_updates", `["message","callback_query"]`)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	var res apiResponse[[]Update]
	if err := c.do(req, &res); err != nil {
		return nil, err
	}
	return res.Result, nil
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	return c.SendMessageWithMarkup(ctx, chatID, text, nil)
}

func (c *Client) SendMessageWithMarkup(ctx context.Context, chatID int64, text string, markup any) error {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.token),
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	var res apiResponse[Message]
	if err := c.do(req, &res); err != nil {
		return err
	}
	return nil
}

func (c *Client) SendPhoto(ctx context.Context, chatID int64, fileID, caption string) error {
	payload := map[string]any{
		"chat_id": chatID,
		"photo":   fileID,
	}
	if caption != "" {
		payload["caption"] = caption
	}
	return c.sendMedia(ctx, "sendPhoto", payload)
}

func (c *Client) SendDocument(ctx context.Context, chatID int64, fileID, caption string) error {
	payload := map[string]any{
		"chat_id":  chatID,
		"document": fileID,
	}
	if caption != "" {
		payload["caption"] = caption
	}
	return c.sendMedia(ctx, "sendDocument", payload)
}

func (c *Client) sendMedia(ctx context.Context, method string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method),
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	var res apiResponse[Message]
	if err := c.do(req, &res); err != nil {
		return err
	}
	return nil
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackID, text string) error {
	payload := map[string]any{
		"callback_query_id": callbackID,
	}
	if text != "" {
		payload["text"] = text
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/bot%s/answerCallbackQuery", c.baseURL, c.token),
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	var res apiResponse[bool]
	if err := c.do(req, &res); err != nil {
		return err
	}
	return nil
}

func (c *Client) EditMessageReplyMarkup(ctx context.Context, chatID int64, messageID int, markup any) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/bot%s/editMessageReplyMarkup", c.baseURL, c.token),
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	var res apiResponse[bool]
	if err := c.do(req, &res); err != nil {
		return err
	}
	return nil
}

type ReplyKeyboardMarkup struct {
	Keyboard              [][]KeyboardButton `json:"keyboard"`
	ResizeKeyboard        bool               `json:"resize_keyboard,omitempty"`
	IsPersistent          bool               `json:"is_persistent,omitempty"`
	OneTimeKeyboard       bool               `json:"one_time_keyboard,omitempty"`
	InputFieldPlaceholder string             `json:"input_field_placeholder,omitempty"`
}

type KeyboardButton struct {
	Text string `json:"text"`
}

type ForceReply struct {
	ForceReply            bool   `json:"force_reply"`
	InputFieldPlaceholder string `json:"input_field_placeholder,omitempty"`
	Selective             bool   `json:"selective,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type apiResponse[T any] struct {
	Ok          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("telegram http status: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	switch v := out.(type) {
	case *apiResponse[[]Update]:
		if !v.Ok {
			return errors.New(v.Description)
		}
	case *apiResponse[Message]:
		if !v.Ok {
			return errors.New(v.Description)
		}
	case *apiResponse[bool]:
		if !v.Ok {
			return errors.New(v.Description)
		}
	}
	return nil
}

type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Message struct {
	MessageID     int            `json:"message_id"`
	From          *User          `json:"from"`
	Chat          Chat           `json:"chat"`
	Text          string         `json:"text"`
	Caption       string         `json:"caption"`
	Photo         []PhotoSize    `json:"photo"`
	Document      *Document      `json:"document"`
	ForwardOrigin *ForwardOrigin `json:"forward_origin"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	LanguageCode string `json:"language_code"`
}

type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type PhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int64  `json:"file_size"`
}

type Document struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileName     string `json:"file_name"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
}

type ForwardOrigin struct {
	Type            string `json:"type"`
	Date            int64  `json:"date"`
	SenderUser      *User  `json:"sender_user"`
	SenderChat      *Chat  `json:"sender_chat"`
	Chat            *Chat  `json:"chat"`
	MessageID       int    `json:"message_id"`
	AuthorSignature string `json:"author_signature"`
	SenderName      string `json:"sender_name"`
}

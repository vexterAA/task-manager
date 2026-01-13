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
	q.Set("allowed_updates", `["message"]`)
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
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
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
	}
	return nil
}

type Update struct {
	UpdateID int      `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	LanguageCode string `json:"language_code"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

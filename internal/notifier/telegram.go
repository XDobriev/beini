package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

type DeliveryError struct {
	Code       int
	RetryAfter time.Duration
	Permanent  bool
}

func (e *DeliveryError) Error() string { return "telegram delivery failed" }

type Telegram struct {
	endpoint, chat string
	client         *http.Client
}

var botToken = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
var groupID = regexp.MustCompile(`^-[1-9][0-9]*$`)

func NewTelegram(token, chat string) (*Telegram, error) {
	if !botToken.MatchString(token) || !groupID.MatchString(chat) {
		return nil, errors.New("требуются корректные TELEGRAM_BOT_TOKEN и отрицательный TELEGRAM_CHAT_ID группы")
	}
	return &Telegram{endpoint: "https://api.telegram.org/bot" + token + "/sendMessage", chat: chat, client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (t *Telegram) Send(ctx context.Context, text string) error {
	body, _ := json.Marshal(map[string]any{"chat_id": t.chat, "text": text, "protect_content": true, "link_preview_options": map[string]bool{"is_disabled": true}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return &DeliveryError{}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return &DeliveryError{}
	} // URL содержит token: исходную ошибку не возвращаем.
	defer resp.Body.Close()
	var result struct {
		OK        bool `json:"ok"`
		ErrorCode int  `json:"error_code"`
		Result    struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result)
	if resp.StatusCode == http.StatusOK && decodeErr == nil && result.OK && result.Result.MessageID > 0 {
		return nil
	}
	code := resp.StatusCode
	if code == 200 && result.ErrorCode != 0 {
		code = result.ErrorCode
	}
	retry := result.Parameters.RetryAfter
	if header, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && header > retry {
		retry = header
	}
	// Защита от переполнения duration в некорректном ответе.
	retry = max(0, min(retry, 86400))
	return &DeliveryError{Code: code, RetryAfter: time.Duration(retry) * time.Second, Permanent: code >= 400 && code < 500 && code != 429 && code != 408}
}

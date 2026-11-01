package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"beini-backend/internal/orderstate"
)

func TestTelegramHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, body         string
		code               int
		success, permanent bool
		retry              time.Duration
	}{
		{"ok", `{"ok":true,"result":{"message_id":12}}`, 200, true, false, 0},
		{"no-message", `{"ok":true,"result":{}}`, 200, false, false, 0},
		{"api-error", `{"ok":false,"error_code":403}`, 200, false, true, 0},
		{"bad-json", `no`, 200, false, false, 0},
		{"server", `{}`, 503, false, false, 0},
		{"forbidden", `{"ok":false}`, 403, false, true, 0},
		{"rate-limit", `{"ok":false,"parameters":{"retry_after":120}}`, 429, false, false, 120 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
					t.Error("wrong request")
				}
				json.NewDecoder(r.Body).Decode(&got)
				w.WriteHeader(tc.code)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			tr, err := NewTelegram("123:secret", "-100123")
			if err != nil {
				t.Fatal(err)
			}
			tr.endpoint = server.URL
			err = tr.Send(context.Background(), "<b>Имя</b> & 🍲")
			if tc.success {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var de *DeliveryError
				if !errors.As(err, &de) || de.Permanent != tc.permanent || de.RetryAfter != tc.retry {
					t.Fatalf("unexpected sanitized error: %v", err)
				}
			}
			if got["parse_mode"] != nil || got["chat_id"] != "-100123" || got["text"] != "<b>Имя</b> & 🍲" || got["protect_content"] != true {
				t.Fatalf("payload: %v", got)
			}
		})
	}
}
func TestTelegramRedirectAndSecret(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	tr, _ := NewTelegram("123:secret", "-1")
	tr.endpoint = redirect.URL
	if tr.Send(context.Background(), "text") == nil || calls != 0 {
		t.Fatal("redirect followed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := tr.Send(ctx, "text")
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "http") {
		t.Fatal("unsafe network error")
	}
	for _, chat := range []string{"", "123", "@public", "-1/invalid"} {
		if _, e := NewTelegram("123:secret", chat); e == nil {
			t.Fatal("accepted non-group destination")
		}
	}
}
func TestLongCardKeepsAllItems(t *testing.T) {
	source := map[string]any{"phone": "+79990000000", "comment": strings.Repeat("🍲", 500)}
	var items []map[string]any
	for i := 0; i < 50; i++ {
		items = append(items, map[string]any{"name": strings.Repeat("Ж", 100) + "END", "price": 10, "amount": 2})
	}
	source["items"] = items
	data, _ := json.Marshal(source)
	parts, err := messages(orderstate.Notification{ID: "uuid:confirmed", Status: orderstate.Confirmed, Source: data})
	if err != nil || len(parts) < 2 {
		t.Fatal("long card not split")
	}
	all := strings.Join(parts, "")
	if strings.Count(all, "END") != 50 {
		t.Fatal("items lost")
	}
	for _, part := range parts {
		if len(utf16.Encode([]rune(part))) > 4096 {
			t.Fatal("Telegram limit exceeded")
		}
	}
}

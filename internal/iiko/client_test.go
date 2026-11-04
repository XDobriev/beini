package iiko

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"beini-backend/internal/config"
	"beini-backend/internal/domain"
)

func TestUpstreamErrorsDoNotExposeResponseBody(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"private":"guest-contact-and-api-secret"} broken`))
			}))
			defer server.Close()
			client := NewClient(&config.Config{APIBaseURL: server.URL}, nil)
			var response AccessTokenResponse
			err := client.postJSON(context.Background(), "/api/v2/access_token", AccessTokenRequest{}, &response, "")
			if err == nil {
				t.Fatal("invalid upstream response must fail")
			}
			if strings.Contains(err.Error(), "guest-contact-and-api-secret") {
				t.Fatal("upstream body leaked into an error")
			}
		})
	}
}

func TestCreateDoesNotLogGuest(t *testing.T) {
	client, _ := newTestClient(t, StatusSuccess, nil)
	var logs bytes.Buffer
	client.logger = slog.New(slog.NewTextHandler(&logs, nil))
	order := testOrder(domain.DeliveryPickUp, "")
	order.CustomerName = "private-guest-marker"
	if _, err := client.CreateDeliveryOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), order.CustomerName) || strings.Contains(logs.String(), order.Phone) {
		t.Fatal("guest details leaked into application logs")
	}
}

// newTestClient поднимает заглушку iiko; byIDStatuses — последовательность статусов для /deliveries/by_id
func newTestClient(t *testing.T, createStatus string, byIDStatuses []string) (*Client, *CreateDeliveryRequest) {
	t.Helper()
	var captured CreateDeliveryRequest
	var byIDCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(AccessTokenResponse{Token: "tok"})
	})
	mux.HandleFunc("/api/1/deliveries/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		resp := CreateDeliveryResponse{CorrelationID: "corr", OrderInfo: OrderInfo{ID: "order-1", CreationStatus: createStatus}}
		if createStatus == StatusError {
			resp.OrderInfo.ErrorInfo = &ErrorInfo{Code: "ProductNotFound", Message: "нет блюда"}
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/1/deliveries/by_id", func(w http.ResponseWriter, r *http.Request) {
		i := int(byIDCalls.Add(1)) - 1
		if i >= len(byIDStatuses) {
			i = len(byIDStatuses) - 1
		}
		info := OrderInfo{ID: "order-1", CreationStatus: byIDStatuses[i]}
		if info.CreationStatus == StatusError {
			info.ErrorInfo = &ErrorInfo{Code: "TerminalGroupUnregistered", Message: "касса не на связи"}
		}
		_ = json.NewEncoder(w).Encode(OrdersByIDResponse{Orders: []OrderInfo{info}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &config.Config{
		APIBaseURL:              srv.URL,
		OrganizationID:          "org",
		TerminalGroupID:         "term",
		DeliveryCity:            "Назрань",
		TransportToFrontTimeout: 60,
		OrderTypes:              config.OrderTypes{DeliveryByCourier: "courier", DeliveryPickUp: "pickup"},
		PaymentTypes:            config.PaymentTypes{Cash: "cash"},
	}
	c := NewClient(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.pollTimeout = 5 * time.Second
	return c, &captured
}

func testOrder(dt domain.DeliveryType, addr string) *domain.Order {
	return &domain.Order{
		CustomerName: "Гость", Phone: "89204827777", DeliveryType: dt, Address: addr,
		Items: []domain.OrderItem{{ProductID: "p1", Price: 590, Amount: 2}},
	}
}

func TestCreateDeliveryOrder_Success(t *testing.T) {
	c, got := newTestClient(t, StatusSuccess, nil)
	order := testOrder(domain.DeliveryByCourier, "ул. Осканова, 21, кв. 5")
	order.Comment = "Тестовый заказ — НЕ ГОТОВИТЬ"
	res, err := c.CreateDeliveryOrder(context.Background(), order)
	if err != nil || res.OrderInfo.CreationStatus != StatusSuccess {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got.CreateOrderSettings.TransportToFrontTimeout != 60 {
		t.Errorf("transportToFrontTimeout = %d; want 60", got.CreateOrderSettings.TransportToFrontTimeout)
	}
	a := got.Order.DeliveryPoint.Address
	if a.Type != "legacy" || a.Street.Name != "Осканова" || a.Street.City != "Назрань" || a.House != "21" || a.Flat != "5" {
		t.Errorf("адрес: %+v", a)
	}
	if got.Order.Phone != "+79204827777" || got.Order.OrderTypeID != "courier" {
		t.Errorf("phone=%q type=%q", got.Order.Phone, got.Order.OrderTypeID)
	}
	if got.Order.Payments[0].Sum != 1180 {
		t.Errorf("сумма оплаты = %v; want 1180", got.Order.Payments[0].Sum)
	}
	if got.Order.Comment != order.Comment || got.Order.Customer.Comment != order.Comment || got.Order.Items[0].Comment != order.Comment {
		t.Errorf("комментарий не передан в заказ, клиента или позицию: %+v", got.Order)
	}
}

func TestCreateDeliveryOrder_PickupHasNoAddress(t *testing.T) {
	c, got := newTestClient(t, StatusSuccess, nil)
	if _, err := c.CreateDeliveryOrder(context.Background(), testOrder(domain.DeliveryPickUp, "")); err != nil {
		t.Fatal(err)
	}
	if got.Order.DeliveryPoint != nil || got.Order.OrderTypeID != "pickup" {
		t.Errorf("самовывоз: point=%v type=%q", got.Order.DeliveryPoint, got.Order.OrderTypeID)
	}
}

func TestCreateDeliveryOrder_InProgressThenSuccess(t *testing.T) {
	c, _ := newTestClient(t, StatusInProgress, []string{StatusInProgress, StatusSuccess})
	res, err := c.CreateDeliveryOrder(context.Background(), testOrder(domain.DeliveryPickUp, ""))
	if err != nil || res.OrderInfo.CreationStatus != StatusSuccess {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestCreateDeliveryOrder_ErrorFromFront(t *testing.T) {
	c, _ := newTestClient(t, StatusInProgress, []string{StatusError})
	_, err := c.CreateDeliveryOrder(context.Background(), testOrder(domain.DeliveryPickUp, ""))
	var oe *OrderError
	if !errors.As(err, &oe) || oe.Info == nil || oe.Info.Code != "TerminalGroupUnregistered" {
		t.Fatalf("ожидалась OrderError с кодом TerminalGroupUnregistered, получено %v", err)
	}
}

func TestCreateDeliveryOrder_ImmediateError(t *testing.T) {
	c, _ := newTestClient(t, StatusError, nil)
	_, err := c.CreateDeliveryOrder(context.Background(), testOrder(domain.DeliveryPickUp, ""))
	var oe *OrderError
	if !errors.As(err, &oe) || oe.Info.Code != "ProductNotFound" {
		t.Fatalf("получено %v", err)
	}
}

func TestCreateDeliveryOrder_StillInProgressAfterTimeout(t *testing.T) {
	c, _ := newTestClient(t, StatusInProgress, []string{StatusInProgress})
	c.pollTimeout = 1500 * time.Millisecond
	res, err := c.CreateDeliveryOrder(context.Background(), testOrder(domain.DeliveryPickUp, ""))
	if err != nil || res.OrderInfo.CreationStatus != StatusInProgress {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestCreateDeliveryOrder_InvalidAddressRejectedBeforeRequest(t *testing.T) {
	c, got := newTestClient(t, StatusSuccess, nil)
	if _, err := c.CreateDeliveryOrder(context.Background(), testOrder(domain.DeliveryByCourier, "Осканова")); err == nil {
		t.Fatal("адрес без дома должен быть отклонён")
	}
	if got.OrganizationID != "" {
		t.Error("запрос в iiko не должен отправляться")
	}
}

func TestGetOrderInfoSelectsRequestedID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/access_token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(AccessTokenResponse{Token: "tok"})
	})
	mux.HandleFunc("/api/1/deliveries/by_id", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(OrdersByIDResponse{Orders: []OrderInfo{
			{ID: "other", CreationStatus: StatusSuccess},
			{ID: "wanted", CreationStatus: StatusInProgress},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(&config.Config{APIBaseURL: srv.URL}, nil)
	info, err := c.GetOrderInfo(context.Background(), "wanted")
	if err != nil || info.CreationStatus != StatusInProgress {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if info, err := c.GetOrderInfo(context.Background(), "missing"); err == nil || info != nil {
		t.Fatalf("чужой заказ принят за наш: %+v %v", info, err)
	}
}

func TestSubmitDoesNotFollowRedirectOrAcceptMismatchedID(t *testing.T) {
	for _, mode := range []string{"redirect", "mismatch", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			var targetCalls atomic.Int32
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v2/access_token", func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(AccessTokenResponse{Token: "tok"})
			})
			mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) })
			mux.HandleFunc("/api/1/deliveries/create", func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
				case "mismatch":
					json.NewEncoder(w).Encode(CreateDeliveryResponse{OrderInfo: OrderInfo{ID: "different", CreationStatus: StatusSuccess}})
				case "malformed":
					io.WriteString(w, `{"orderInfo":`)
				}
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			c := NewClient(&config.Config{APIBaseURL: srv.URL}, nil)
			resp, err := c.SubmitDeliveryOrder(context.Background(), testOrder(domain.DeliveryPickUp, ""), "saved-id")
			if err == nil || resp != nil || targetCalls.Load() != 0 {
				t.Fatalf("resp=%+v err=%v redirects=%d", resp, err, targetCalls.Load())
			}
		})
	}
}

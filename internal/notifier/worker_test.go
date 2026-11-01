package notifier

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"beini-backend/internal/orderstate"
)

type fakeTransport struct {
	calls []string
	fail  bool
}

func (f *fakeTransport) Send(_ context.Context, text string) error {
	f.calls = append(f.calls, text)
	if f.fail {
		return errors.New("offline")
	}
	return nil
}
func TestDeliveryRetryRestartAndOrdering(t *testing.T) {
	orders := orderstate.NewMemory()
	defer orders.Close()
	rec, _, _ := orders.ReserveWithNotification("key", "body", 100, `{"phone":"+79990000000","items":[{"name":"Суп","price":100,"amount":1}]}`)
	rec.Status = orderstate.Confirmed
	if err := orders.Update(rec); err != nil {
		t.Fatal(err)
	}
	orders.Release(rec)
	path := filepath.Join(t.TempDir(), "delivery.jsonl")
	ledger, err := orderstate.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tr := &fakeTransport{fail: true}
	now := time.Now()
	w := New(orders, ledger, tr, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.now = func() time.Time { return now }
	w.Step(context.Background())
	if len(tr.calls) != 1 {
		t.Fatalf("final bypassed pending: %v", tr.calls)
	}
	w.Step(context.Background())
	if len(tr.calls) != 1 {
		t.Fatal("retry ignored backoff")
	}
	ledger.Close()
	ledger, err = orderstate.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	tr.fail = false
	w = New(orders, ledger, tr, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.now = func() time.Time { return now }
	w.Step(context.Background())
	if len(tr.calls) != 1 {
		t.Fatal("restart lost retry deadline")
	}
	now = now.Add(time.Hour)
	w.Step(context.Background())
	if len(tr.calls) != 3 {
		t.Fatalf("pending and final not delivered: %v", tr.calls)
	}
	w.Step(context.Background())
	if len(tr.calls) != 3 {
		t.Fatal("duplicate acked event")
	}
	ledger.Close()
	ledger, err = orderstate.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	w = New(orders, ledger, tr, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.Step(context.Background())
	if len(tr.calls) != 3 {
		t.Fatal("restart lost ack")
	}
}

type transportFunc func(context.Context, string) error

func (f transportFunc) Send(c context.Context, s string) error { return f(c, s) }
func TestDeliveryAckFailureLeavesReplayableEvent(t *testing.T) {
	orders := orderstate.NewMemory()
	defer orders.Close()
	rec, _, _ := orders.ReserveWithNotification("key", "body", 100, `{"phone":"+79990000000","items":[]}`)
	orders.Release(rec)
	path := filepath.Join(t.TempDir(), "receipt.jsonl")
	ledger, err := orderstate.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sends := 0
	tr := transportFunc(func(context.Context, string) error { sends++; ledger.Close(); return nil })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := New(orders, ledger, tr, logger)
	w.Step(context.Background())
	ledger, err = orderstate.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	tr = func(context.Context, string) error { sends++; return nil }
	w = New(orders, ledger, tr, logger)
	w.now = func() time.Time { return time.Now().Add(time.Hour) }
	w.Step(context.Background())
	if sends != 2 {
		t.Fatal("unacknowledged success lost instead of retried")
	}
	if _, ok, err := orders.Lookup("key"); !ok || err != nil {
		t.Fatal("receipt failure damaged order store")
	}
}
func TestMultipartRetriesOnlyUnacknowledgedParts(t *testing.T) {
	orders := orderstate.NewMemory()
	defer orders.Close()
	source := `{"phone":"+79990000000","comment":"` + strings.Repeat("A", 8000) + `","items":[]}`
	rec, _, _ := orders.ReserveWithNotification("key", "body", 100, source)
	rec.Status = orderstate.Rejected
	orders.Update(rec)
	orders.Release(rec)
	ledger := orderstate.NewMemory()
	defer ledger.Close()
	var calls []string
	fail := true
	tr := transportFunc(func(_ context.Context, s string) error {
		calls = append(calls, s)
		if fail && strings.Contains(s, "часть 2/") {
			return errors.New("fail")
		}
		return nil
	})
	w := New(orders, ledger, tr, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now()
	w.now = func() time.Time { return now }
	w.Step(context.Background())
	if len(calls) != 2 {
		t.Fatal("terminal bypassed partial pending")
	}
	fail = false
	now = now.Add(time.Hour)
	w.Step(context.Background())
	countFirst := 0
	for _, s := range calls {
		if strings.Contains(s, ":pending • часть 1/") {
			countFirst++
		}
	}
	if countFirst != 1 || !strings.Contains(calls[len(calls)-1], ":rejected") {
		t.Fatal("multipart ack/order violated")
	}
}
func TestConcurrentStepsDeduplicate(t *testing.T) {
	orders := orderstate.NewMemory()
	defer orders.Close()
	rec, _, _ := orders.ReserveWithNotification("key", "body", 0, `{}`)
	orders.Release(rec)
	ledger := orderstate.NewMemory()
	defer ledger.Close()
	tr := &fakeTransport{}
	w := New(orders, ledger, tr, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var group sync.WaitGroup
	for i := 0; i < 10; i++ {
		group.Add(1)
		go func() { defer group.Done(); w.Step(context.Background()) }()
	}
	group.Wait()
	if len(tr.calls) != 1 {
		t.Fatal("concurrent steps duplicated event")
	}
}
func TestTransportPanicAndPermanentErrorKeepEvents(t *testing.T) {
	for _, panicSend := range []bool{true, false} {
		orders := orderstate.NewMemory()
		rec, _, _ := orders.ReserveWithNotification("key", "body", 0, `{}`)
		orders.Release(rec)
		ledger := orderstate.NewMemory()
		tr := transportFunc(func(context.Context, string) error {
			if panicSend {
				panic("transport panic")
			}
			return &DeliveryError{Permanent: true, Code: 403}
		})
		w := New(orders, ledger, tr, slog.New(slog.NewTextHandler(io.Discard, nil)))
		now := time.Now()
		w.now = func() time.Time { return now }
		w.Step(context.Background())
		receipt, ok, err := ledger.Lookup(rec.ID + ":pending:part:0")
		if err != nil || !ok || receipt.Status == orderstate.Confirmed || !receipt.DeliveryNextAt.After(now) {
			t.Fatal("failure dropped event")
		}
		if !panicSend && receipt.DeliveryNextAt.Before(now.Add(15*time.Minute)) {
			t.Fatal("permanent error spins")
		}
		orders.Close()
		ledger.Close()
	}
}

func TestRateLimitPausesDestinationAcrossOrdersAndRestart(t *testing.T) {
	orders := orderstate.NewMemory()
	defer orders.Close()
	for _, key := range []string{"one", "two"} {
		r, _, _ := orders.ReserveWithNotification(key, "body", 0, `{}`)
		orders.Release(r)
	}
	path := filepath.Join(t.TempDir(), "receipt.jsonl")
	ledger, err := orderstate.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ledger.Close() }()
	sends := 0
	tr := transportFunc(func(context.Context, string) error {
		sends++
		return &DeliveryError{Code: 429, RetryAfter: 120 * time.Second}
	})
	now := time.Now()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := New(orders, ledger, tr, logger)
	w.now = func() time.Time { return now }
	w.Step(context.Background())
	if sends != 1 {
		t.Fatal("429 ignored for second order")
	}
	ledger.Close()
	ledger, err = orderstate.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	w = New(orders, ledger, tr, logger)
	w.now = func() time.Time { return now.Add(time.Minute) }
	w.Step(context.Background())
	if sends != 1 {
		t.Fatal("restart lost destination cooldown")
	}
}

func TestRetryDeadlineStartsAfterTransportResponse(t *testing.T) {
	for _, retryAfter := range []time.Duration{0, 5 * time.Second} {
		orders := orderstate.NewMemory()
		defer orders.Close()
		rec, _, err := orders.ReserveWithNotification("key", "body", 0, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		orders.Release(rec)
		ledger := orderstate.NewMemory()
		defer ledger.Close()
		now := time.Now()
		sends := 0
		tr := transportFunc(func(context.Context, string) error {
			sends++
			now = now.Add(8 * time.Second)
			return &DeliveryError{Code: 429, RetryAfter: retryAfter}
		})
		w := New(orders, ledger, tr, slog.New(slog.NewTextHandler(io.Discard, nil)))
		w.now = func() time.Time { return now }
		w.Step(context.Background())
		want := now.Add(max(2*time.Second, retryAfter))
		for _, key := range []string{rec.ID + ":pending:part:0", "transport-cooldown"} {
			receipt, ok, err := ledger.Lookup(key)
			if err != nil || !ok || !receipt.DeliveryNextAt.Equal(want) {
				t.Fatalf("retry deadline must start after response: key=%s got=%v want=%v", key, receipt.DeliveryNextAt, want)
			}
		}
		w.Step(context.Background())
		if sends != 1 {
			t.Fatal("request duration consumed retry pause")
		}
	}
}

type interruptedRetryStore struct {
	*orderstate.Store
	afterSend bool
}

func (s *interruptedRetryStore) SaveDelivery(key string, attempts int, next time.Time, delivered bool) error {
	if err := s.Store.SaveDelivery(key, attempts, next, delivered); err != nil {
		return err
	}
	if s.afterSend {
		panic("stop after first persisted retry update")
	}
	return nil
}

func TestCrashBetweenRetryWritesPreservesDestinationPause(t *testing.T) {
	orders := orderstate.NewMemory()
	defer orders.Close()
	for _, key := range []string{"first", "second"} {
		rec, _, err := orders.ReserveWithNotification(key, "body", 0, `{}`)
		if err != nil {
			t.Fatal(err)
		}
		orders.Release(rec)
	}
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	ledger, err := orderstate.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ledger.Close() }()
	interrupted := &interruptedRetryStore{Store: ledger}
	sends := 0
	now := time.Now()
	tr := transportFunc(func(context.Context, string) error {
		sends++
		interrupted.afterSend = true
		return &DeliveryError{Code: 429, RetryAfter: 120 * time.Second}
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := New(orders, interrupted, tr, logger)
	w.now = func() time.Time { return now }
	w.Step(context.Background())
	ledger.Close()
	ledger, err = orderstate.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tr = func(context.Context, string) error { sends++; return nil }
	w = New(orders, ledger, tr, logger)
	w.now = func() time.Time { return now.Add(time.Second) }
	w.Step(context.Background())
	if sends != 1 {
		t.Fatal("restart allowed another order to bypass persisted 429 pause")
	}
}

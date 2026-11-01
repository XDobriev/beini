package orderstate

import (
	"path/filepath"
	"testing"
)

func TestNotificationSourceAndQueueSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	const source = `{"phone":"+79990000000","deliveryType":"pickup","items":[{"productId":"one","amount":2,"price":620}]}`
	r, fresh, err := s.ReserveWithNotification("private-key", "hash", 1240, source)
	if err != nil || !fresh {
		t.Fatalf("reserve: %v", err)
	}
	queue, err := s.NotificationQueue()
	if err != nil || len(queue) != 1 || queue[0].Status != Pending || string(queue[0].Source) != source {
		t.Fatalf("pending queue: %+v %v", queue, err)
	}
	pendingID := queue[0].ID
	r.Status = Confirmed
	r.CreationStatus = "Success"
	if err = s.Update(r); err != nil {
		t.Fatal(err)
	}
	s.Release(r)
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	queue, err = s.NotificationQueue()
	if err != nil || len(queue) != 2 || queue[0].ID != pendingID || queue[1].Status != Confirmed || string(queue[1].Source) != source {
		t.Fatalf("restored queue: %+v %v", queue, err)
	}
	if _, fresh, err = s.ReserveWithNotification("private-key", "hash", 1240, `{}`); err != nil || fresh {
		t.Fatal("replay reserved second delivery")
	}
	again, _ := s.NotificationQueue()
	if len(again) != 2 || again[1].ID != queue[1].ID {
		t.Fatal("unstable notification IDs")
	}
}

package orderstate

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDeliverySyncFailureDoesNotAck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delivery.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SaveDelivery("event", 1, time.Now().Add(time.Minute), false); err != nil {
		t.Fatal(err)
	}
	s.file.Close()
	if err = s.SaveDelivery("event", 1, time.Time{}, true); err == nil {
		t.Fatal("ack succeeded without fsync")
	}
	if s.records[Hash([]byte("event"))].Status == Confirmed {
		t.Fatal("unpersisted ack in memory")
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec, _, err := s.Lookup("event")
	if err != nil || rec.Status != Pending || rec.DeliveryAttempts != 1 {
		t.Fatal("restart lost unacknowledged attempt")
	}
}

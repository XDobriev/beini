package orderstate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRecoveryAndExclusiveWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, fresh, err := s.Reserve("secret-key", "body-hash", 590)
	if err != nil || !fresh {
		t.Fatalf("fresh=%v err=%v", fresh, err)
	}
	if second, err := Open(path); err == nil {
		second.Close()
		t.Fatal("второй writer получил доступ")
	}
	// Незавершённая попытка переживает остановку, включая active=true в памяти.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	recovered, fresh, err := s.Reserve("secret-key", "body-hash", 999)
	if err != nil || fresh || recovered.ID != rec.ID || recovered.TotalSum != 590 || !recovered.OutcomeUnknown {
		t.Fatalf("recovered=%+v fresh=%v err=%v", recovered, fresh, err)
	}
	if _, _, err := s.Reserve("secret-key", "changed", 590); !errors.Is(err, ErrMismatch) {
		t.Fatal(err)
	}
	pending := s.AcquirePending()
	if len(pending) != 1 {
		t.Fatalf("pending=%v", pending)
	}
	if len(s.AcquirePending()) != 0 {
		t.Fatal("двойное получение pending")
	}
	recovered.Status = Confirmed
	recovered.CreationStatus = "Success"
	recovered.OutcomeUnknown = false
	if err := s.Update(recovered); err != nil {
		t.Fatal(err)
	}
	s.Release(recovered)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err := s.Lookup("secret-key")
	if err != nil || !ok || got.Status != Confirmed || len(s.AcquirePending()) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	// Проверяем отсутствие исходного bearer key в журнале (после закрытия OS lock).
	s.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("secret-key")) {
		t.Fatal("исходный ключ попал на диск")
	}
}

func TestTornTailAndCorruption(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial-tail", true: "complete-corruption"}[corrupt], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "orders.jsonl")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			rec, _, err := s.Reserve("key", "hash", 590)
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			tail := []byte(`{"record":`)
			if corrupt {
				tail = append(tail, '\n')
			}
			if _, err := f.Write(tail); err != nil {
				t.Fatal(err)
			}
			f.Close()
			s, err = Open(path)
			if corrupt {
				if err == nil {
					s.Close()
					t.Fatal("повреждение полной записи проигнорировано")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got, fresh, err := s.Reserve("key", "hash", 590)
			if err != nil || fresh || got.ID != rec.ID {
				t.Fatalf("%+v fresh=%v err=%v", got, fresh, err)
			}
			// После восстановления возможна новая корректная запись и повторное чтение.
			if _, _, err := s.Reserve("other", "other", 10); err != nil {
				t.Fatal(err)
			}
			s.Close()
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
		})
	}
}

func TestWriteFailurePoisonsStore(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "orders.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Имитируем недоступность диска/дескриптора в точке записи.
	s.file.Close()
	if _, _, err := s.Reserve("key", "hash", 590); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, _, err := s.Lookup("key"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, _, err := s.Reserve("another", "hash", 590); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestConcurrentReserveHasSingleSender(t *testing.T) {
	s := NewMemory()
	var senders atomic.Int32
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, fresh, err := s.Reserve("same", "same", 590)
			if err != nil {
				t.Error(err)
			}
			if fresh {
				senders.Add(1)
			}
		}()
	}
	wg.Wait()
	if senders.Load() != 1 {
		t.Fatal(senders.Load())
	}
}

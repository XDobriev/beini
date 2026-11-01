// Package orderstate хранит попытки создания заказа до обращения к iiko.
package orderstate

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	Pending   = "pending"
	Confirmed = "confirmed"
	Rejected  = "rejected"
)

var (
	ErrMismatch    = errors.New("ключ уже использован для другого тела")
	ErrUnavailable = errors.New("хранилище заказов недоступно")
)

// Record хранит состояние и защищённый источник будущих уведомлений.
// Исходный ключ клиента не сохраняется; NotificationSource не возвращается HTTP.
type Record struct {
	NotificationSource string    `json:"notificationSource,omitempty"`
	KeyHash            string    `json:"keyHash"`
	PayloadHash        string    `json:"payloadHash"`
	ID                 string    `json:"id"`
	IikoID             string    `json:"iikoId"`
	CorrelationID      string    `json:"correlationId"`
	Status             string    `json:"status"`
	CreationStatus     string    `json:"creationStatus"`
	OutcomeUnknown     bool      `json:"outcomeUnknown"`
	TotalSum           float64   `json:"totalSum"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type entry struct {
	Record   json.RawMessage `json:"record"`
	Checksum string          `json:"sha256"`
}

// Store — один writer; файловая версия удерживает OS lock до Close.
// Записи не истекают: очистка ключа могла бы разрешить повторное создание.
type Store struct {
	mu      sync.Mutex
	records map[string]Record
	active  map[string]bool
	file    *os.File
	failed  bool
	closed  bool
}

func NewMemory() *Store {
	return &Store{records: make(map[string]Record), active: make(map[string]bool)}
}

// Open открывает журнал, проверяет checksum каждой полной записи и отсекает
// только незавершённый хвост. Ошибка полной записи блокирует запуск.
func Open(path string) (*Store, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	s := NewMemory()
	s.file = f
	ok := false
	defer func() {
		if !ok {
			s.Close()
		}
	}()
	r := bufio.NewReader(f)
	var offset int64
	for {
		line, readErr := r.ReadBytes('\n')
		if readErr == io.EOF {
			if len(line) > 0 {
				if err = f.Truncate(offset); err != nil {
					return nil, err
				}
				if err = f.Sync(); err != nil {
					return nil, err
				}
			}
			break
		}
		if readErr != nil {
			return nil, readErr
		}
		var e entry
		if err = json.Unmarshal(line, &e); err != nil || Hash(e.Record) != e.Checksum {
			return nil, fmt.Errorf("повреждение журнала заказов на смещении %d", offset)
		}
		var rec Record
		if err = json.Unmarshal(e.Record, &rec); err != nil || rec.KeyHash == "" || rec.ID == "" ||
			(rec.Status != Pending && rec.Status != Confirmed && rec.Status != Rejected) {
			return nil, fmt.Errorf("некорректная запись журнала на смещении %d", offset)
		}
		s.records[rec.KeyHash] = rec
		offset += int64(len(line))
	}
	if _, err = f.Seek(0, io.SeekEnd); err != nil {
		return nil, err
	}
	ok = true
	return s, nil
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func NewID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16]), nil
}

func (s *Store) Lookup(key string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.failed {
		return Record{}, false, ErrUnavailable
	}
	r, ok := s.records[Hash([]byte(key))]
	return r, ok, nil
}

// Reserve фиксирует ID до отправки. fresh=true даёт право на единственный create.
// При восстановлении такого права нет даже для заказа, не успевшего уйти в сеть.
func (s *Store) Reserve(key, payloadHash string, total float64) (rec Record, fresh bool, err error) {
	return s.ReserveWithNotification(key, payloadHash, total, "")
}

// ReserveWithNotification атомарно сохраняет состав/контакты вместе с pending.
// Журнал содержит персональные данные и должен находиться вне web root/Git.
func (s *Store) ReserveWithNotification(key, payloadHash string, total float64, source string) (rec Record, fresh bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.failed {
		return rec, false, ErrUnavailable
	}
	k := Hash([]byte(key))
	if rec, ok := s.records[k]; ok {
		if rec.PayloadHash != payloadHash {
			return rec, false, ErrMismatch
		}
		return rec, false, nil
	}
	id, err := NewID()
	if err != nil {
		return rec, false, err
	}
	now := time.Now().UTC()
	rec = Record{NotificationSource: source, KeyHash: k, PayloadHash: payloadHash, ID: id, IikoID: id, Status: Pending,
		OutcomeUnknown: true, TotalSum: total, CreatedAt: now, UpdatedAt: now}
	if err = s.save(rec); err != nil {
		return rec, false, err
	}
	s.active[k] = true
	return rec, true, nil
}

// save вызывается под mu; память обновляется только после успешного fsync.
func (s *Store) save(rec Record) error {
	if s.file != nil {
		data, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		line, err := json.Marshal(entry{Record: data, Checksum: Hash(data)})
		if err != nil {
			return err
		}
		line = append(line, '\n')
		n, err := s.file.Write(line)
		if err == nil && n != len(line) {
			err = io.ErrShortWrite
		}
		if err == nil {
			err = s.file.Sync()
		}
		if err != nil {
			s.failed = true
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}
	s.records[rec.KeyHash] = rec
	return nil
}

// Update сохраняет результат текущего владельца; терминальные состояния неизменны.
func (s *Store) Update(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.failed {
		return ErrUnavailable
	}
	old, ok := s.records[rec.KeyHash]
	if !ok || old.ID != rec.ID || !s.active[rec.KeyHash] {
		return errors.New("нет права обновления заказа")
	}
	if old.Status != Pending {
		return nil
	}
	if rec.Status != Pending && rec.Status != Confirmed && rec.Status != Rejected {
		return errors.New("неверный статус")
	}
	rec.NotificationSource = old.NotificationSource // Неизменяемый источник после Reserve.
	if old == rec {
		return nil
	}
	rec.UpdatedAt = time.Now().UTC()
	return s.save(rec)
}

func (s *Store) Release(rec Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, rec.KeyHash)
}

// AcquirePending предотвращает гонку опроса с create и другими worker.
func (s *Store) AcquirePending() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.failed {
		return nil
	}
	var records []Record
	for key, rec := range s.records {
		if rec.Status == Pending && !s.active[key] {
			s.active[key] = true
			records = append(records, rec)
		}
	}
	return records
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

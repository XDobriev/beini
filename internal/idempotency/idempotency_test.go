package idempotency

import (
	"net/http"
	"testing"
	"time"
)

func TestIdempotency_SuccessAndReplay(t *testing.T) {
	store := NewStore(10 * time.Minute)
	defer store.Close()
	key := "test-key-1"
	hash := "hash-payload-aaa"

	// 1. Первый запрос: резервирует ключ
	cached, complete, release, err := store.Lock(key, hash)
	if err != nil {
		t.Fatalf("Lock error: %v", err)
	}
	if cached != nil {
		t.Fatalf("ожидался nil cached, получено %+v", cached)
	}
	defer release()

	// 2. Параллельный запрос с тем же ключом во время pending -> конфликт
	_, _, _, errConflict := store.Lock(key, hash)
	if errConflict != ErrConflict {
		t.Fatalf("ожидался ErrConflict, получено %v", errConflict)
	}

	// 3. Завершаем первый запрос
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	complete(http.StatusOK, header, []byte(`{"orderId":"123"}`))

	// 4. Повторный запрос с тем же ключом и тем же хэшем -> должен вернуть кэшированный ответ
	cached2, _, _, err2 := store.Lock(key, hash)
	if err2 != nil {
		t.Fatalf("повторный Lock вернул ошибку: %v", err2)
	}
	if cached2 == nil {
		t.Fatalf("ожидался кэшированный ответ")
	}
	if cached2.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d; want 200", cached2.StatusCode)
	}
	if string(cached2.Body) != `{"orderId":"123"}` {
		t.Errorf("Body = %s; want `{\"orderId\":\"123\"}`", string(cached2.Body))
	}
}

func TestIdempotency_PayloadMismatch(t *testing.T) {
	store := NewStore(10 * time.Minute)
	defer store.Close()
	key := "test-key-mismatch"

	// Первый заказ с одним телом
	_, complete, release, _ := store.Lock(key, "hash-order-1")
	complete(http.StatusOK, nil, []byte("ok-1"))
	release()

	// Повтор того же ключа с ДРУГИМ телом -> ErrPayloadMismatch (RFC 9651)
	_, _, _, err := store.Lock(key, "hash-order-2")
	if err != ErrPayloadMismatch {
		t.Errorf("ожидался ErrPayloadMismatch при смене тела запроса, получено: %v", err)
	}
}

func TestIdempotency_ReleaseOnFailure(t *testing.T) {
	store := NewStore(10 * time.Minute)
	defer store.Close()
	key := "test-key-fail"

	cached, _, release, err := store.Lock(key, "hash-1")
	if err != nil || cached != nil {
		t.Fatalf("первый Lock не удался: err=%v, cached=%v", err, cached)
	}

	// Откатываем (например, при внутренней ошибке)
	release()

	// Теперь этот ключ снова свободен
	cached2, complete2, release2, err2 := store.Lock(key, "hash-1")
	if err2 != nil || cached2 != nil {
		t.Fatalf("повторный Lock после release должен пройти: err=%v", err2)
	}
	defer release2()
	complete2(http.StatusOK, nil, []byte("ok"))
}

func TestIdempotency_InvalidKey(t *testing.T) {
	store := NewStore(10 * time.Minute)
	defer store.Close()

	if _, _, _, err := store.Lock("", "hash"); err != ErrInvalidKey {
		t.Errorf("пустой ключ должен возвращать ErrInvalidKey, получено: %v", err)
	}

	tooLong := string(make([]byte, 130))
	if _, _, _, err := store.Lock(tooLong, "hash"); err != ErrInvalidKey {
		t.Errorf("слишком длинный ключ должен возвращать ErrInvalidKey, получено: %v", err)
	}
}

func TestIdempotency_TTLExpire(t *testing.T) {
	store := NewStore(50 * time.Millisecond)
	defer store.Close()
	key := "test-expire"

	_, complete, release, _ := store.Lock(key, "hash")
	complete(200, nil, []byte("data"))
	release()

	time.Sleep(70 * time.Millisecond)

	// После истечения TTL запись удаляется
	cached, complete2, release2, err := store.Lock(key, "hash")
	if err != nil {
		t.Fatalf("ошибка Lock: %v", err)
	}
	if cached != nil {
		t.Errorf("запись должна была устареть, но вернулся кэш")
	}
	defer release2()
	complete2(200, nil, []byte("data2"))
}

package catalog

import (
	"testing"
)

func TestCatalog_FindProduct(t *testing.T) {
	c := DefaultBeiniCatalog()

	// 1. Поиск по IikoID
	item, ok := c.FindProduct("0ce65922-6a9f-453a-bb92-b7c35b4950bb")
	if !ok {
		t.Fatalf("блюдо не найдено по IikoID")
	}
	if item.Name != "Люля-кебаб из телятины" || item.Price != 590 {
		t.Errorf("неверные данные блюда: %+v", item)
	}

	// 2. Поиск по строковому ID
	item2, ok := c.FindProduct("beini-grill-01")
	if !ok {
		t.Fatalf("блюдо не найдено по ID")
	}
	if item2.IikoID != "0ce65922-6a9f-453a-bb92-b7c35b4950bb" || item2.Price != 590 {
		t.Errorf("неверные данные блюда: %+v", item2)
	}

	// 3. Регистронезависимость и пробелы
	item3, ok := c.FindProduct("  BEINI-GRILL-01  ")
	if !ok || item3.Price != 590 {
		t.Errorf("поиск с пробелами/регистром не сработал")
	}

	// 4. Несуществующее блюдо
	_, ok = c.FindProduct("unknown-id-12345")
	if ok {
		t.Errorf("несуществующее блюдо найдено")
	}

	// 5. Пустой ID
	_, ok = c.FindProduct("")
	if ok {
		t.Errorf("пустой ID найден")
	}
}

func TestMemoryCatalog_Custom(t *testing.T) {
	c := NewMemoryCatalog([]Item{
		{ID: "test-item", IikoID: "uuid-1", Name: "Тест", Price: 100},
	})

	it, ok := c.FindProduct("uuid-1")
	if !ok || it.Price != 100 {
		t.Fatalf("ожидалось 100 руб, получено %+v, ok=%v", it, ok)
	}
}

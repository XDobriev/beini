package catalog

import (
	"strings"
	"sync"
)

// Item описывает блюдо в серверном каталоге номенклатуры BEINI
type Item struct {
	ID     string  `json:"id"`
	IikoID string  `json:"iikoId"`
	Name   string  `json:"name"`
	Price  float64 `json:"price"`
}

// Catalog предоставляет интерфейс для получения проверенных цен и наименований блюд
type Catalog interface {
	FindProduct(idOrIikoID string) (Item, bool)
}

// Inverted/Map каталог для быстрого поиска по ID и IikoID
type MemoryCatalog struct {
	mu    sync.RWMutex
	items map[string]Item
}

// NewMemoryCatalog создает каталог с переданными позициями
func NewMemoryCatalog(items []Item) *MemoryCatalog {
	c := &MemoryCatalog{
		items: make(map[string]Item, len(items)*2),
	}
	for _, it := range items {
		c.addItem(it)
	}
	return c
}

func (c *MemoryCatalog) addItem(it Item) {
	if it.ID != "" {
		c.items[strings.ToLower(strings.TrimSpace(it.ID))] = it
	}
	if it.IikoID != "" {
		c.items[strings.ToLower(strings.TrimSpace(it.IikoID))] = it
	}
}

// FindProduct ищет блюдо по ID или IikoID (регистронезависимо)
func (c *MemoryCatalog) FindProduct(idOrIikoID string) (Item, bool) {
	key := strings.ToLower(strings.TrimSpace(idOrIikoID))
	if key == "" {
		return Item{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	it, ok := c.items[key]
	return it, ok
}

// DefaultBeiniCatalog возвращает актуальный каталог ресторана BEINI
func DefaultBeiniCatalog() *MemoryCatalog {
	return NewMemoryCatalog([]Item{
		// Мангал & Стейки
		{ID: "beini-grill-01", IikoID: "0ce65922-6a9f-453a-bb92-b7c35b4950bb", Name: "Люля-кебаб из телятины", Price: 590},
		{ID: "beini-grill-02", IikoID: "a6c8c806-47c3-429a-bfca-572b9ea16ad6", Name: "Шашлык из баранины", Price: 800},
		{ID: "beini-grill-03", IikoID: "845e4c4e-588a-4819-a742-acf535003574", Name: "Шашлык из говядины", Price: 810},
		{ID: "beini-grill-04", IikoID: "17ba6a67-df70-419c-ad18-c4c8ba3bf9fc", Name: "Стейк «Рибай»", Price: 1250},
		{ID: "beini-grill-05", IikoID: "ab871f62-beee-4925-9e37-f1ea8040d4e7", Name: "Корейка баранина на углях", Price: 1250},
		{ID: "beini-grill-06", IikoID: "68e00f67-0c1e-4e78-87a2-74a718103126", Name: "Садж Ассорти", Price: 3000},

		// Восточная & Национальная
		{ID: "beini-east-01", IikoID: "0c2f2841-e64a-4d1a-8b18-40387aea2332", Name: "Байский плов", Price: 1500},
		{ID: "beini-east-02", IikoID: "0ee7147c-99b9-4cd4-9926-6d03e127c374", Name: "Казан кебаб", Price: 940},
		{ID: "beini-east-03", IikoID: "61418da0-0b48-400d-996b-262535e308f5", Name: "Ковурма лагман", Price: 480},
		{ID: "beini-east-04", IikoID: "92295cfb-001a-424f-8226-cd875373cf40", Name: "Ч1апильг с творогом", Price: 200},
		{ID: "beini-east-05", IikoID: "479e8eeb-1440-4c6e-aeff-0173758da442", Name: "Дулх хьалтам с сушеным мясом", Price: 1060},

		// Супы & Том-Ям
		{ID: "beini-soup-01", IikoID: "d8adaf66-c755-414e-8b6b-7a6c6eea4328", Name: "Тайский суп Том-Ям", Price: 600},
		{ID: "beini-soup-02", IikoID: "ce554b36-f33b-4a9d-a578-6e2c113ab1f7", Name: "Шурпа с бараниной", Price: 490},
		{ID: "beini-soup-03", IikoID: "d6688132-5920-4121-9120-ce1acaea07c7", Name: "Узбекский лагман", Price: 480},
		{ID: "beini-soup-04", IikoID: "234a164e-d3c9-49e2-8ac8-b9d9de932555", Name: "Куриный суп с лапшой", Price: 350},

		// Салаты & Закуски
		{ID: "beini-salad-01", IikoID: "19624fd5-28d8-4489-81dd-4127f2471093", Name: "Салат с авокадо и томатами", Price: 595},
		{ID: "beini-salad-02", IikoID: "b4283e38-e2b8-4f3c-b099-7ce2a9cbb79b", Name: "Хрустящий баклажан с мандаринами", Price: 495},
		{ID: "beini-salad-03", IikoID: "a6f7e66d-58f2-48af-b5b1-7dda59dc1919", Name: "Фирменный салат «Бейни»", Price: 690},
		{ID: "beini-salad-04", IikoID: "4b0f8db8-bafe-46e3-93da-f6f2a88a6b78", Name: "Цезарь с креветками", Price: 645},

		// Суши & Роллы
		{ID: "beini-sushi-01", IikoID: "f587b728-f064-4071-9568-ece89e63e369", Name: "Фирменный ролл «Бейни»", Price: 695},
		{ID: "beini-sushi-02", IikoID: "2d3a5fff-be04-4e24-b573-0b754c10a82f", Name: "Филадельфия Премиум", Price: 450},
		{ID: "beini-sushi-03", IikoID: "85abec96-3a18-4423-b027-d0a49e017884", Name: "Сливочный угорь", Price: 450},
		{ID: "beini-sushi-04", IikoID: "68da6741-f428-48a1-bfc1-403037a9b3a3", Name: "Калифорния с креветками", Price: 450},

		// Пицца & Выпечка
		{ID: "beini-pizza-01", IikoID: "8e63f4a8-5ac5-4dc1-9e9c-2de8b4246ab1", Name: "Пицца Пепперони", Price: 550},
		{ID: "beini-pizza-02", IikoID: "135c11de-49ed-43dc-a9a5-a04a89e23a3c", Name: "Пицца Четыре сыра", Price: 550},
		{ID: "beini-pizza-03", IikoID: "dcf89927-b754-4e81-b11e-4b951a8dc1d4", Name: "Пицца Цезарь", Price: 550},
		{ID: "beini-pizza-04", IikoID: "ccb92a01-1252-4a97-a13e-82c0d0b11f13", Name: "Пицца Маргарита", Price: 500},

		// Десерты
		{ID: "beini-des-01", IikoID: "e92be764-c11e-4fb6-9cdc-fe34a866cfa7", Name: "Чизкейк Сан Себастьян", Price: 350},
		{ID: "beini-des-02", IikoID: "2597f13c-16fd-463c-87fb-4c950fd59aca", Name: "Шоколадный фондан", Price: 340},
		{ID: "beini-des-03", IikoID: "6011dcbc-32ab-456e-b9ec-acf1762ef037", Name: "Наполеон фисташковый", Price: 250},
		{ID: "beini-des-04", IikoID: "3e410594-6f0a-48a3-9f6c-17dee220de13", Name: "Торт блинный", Price: 350},

		// Айс-кофе & Бар
		{ID: "beini-drink-01", IikoID: "8682b4fc-29fb-4e92-9fd8-ff1d5b632e6e", Name: "Айс кофе с урбечом", Price: 420},
		{ID: "beini-drink-02", IikoID: "4717da0b-1590-43f8-a00e-b611792ce315", Name: "Бамбл кофе", Price: 410},
		{ID: "beini-drink-03", IikoID: "0202f964-b181-404a-a90d-52e8b2675878", Name: "Манго-маракуйя лимонад (графин)", Price: 650},
		{ID: "beini-drink-04", IikoID: "7d7120be-356d-4ba9-a35d-1bda7da9c2c9", Name: "Облепиха-маракуйя авторский чай", Price: 380},

		// Тестовый Латте и mock-тестовые позиции
		{ID: "latte", IikoID: "c690faff-db1b-4b51-93cf-f89a461f71a9", Name: "Латте", Price: 250},
		{ID: "p1", IikoID: "p1", Name: "Тестовое блюдо p1", Price: 590},
	})
}

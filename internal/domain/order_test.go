package domain

	"beini-backend/internal/catalog"
	"strings"
	"testing"
)

func TestNormalizePhone(t *testing.T) {
	ok := map[string]string{
		"+7 920 482-77-77":  "+79204827777",
		"8 (920) 482-77-77": "+79204827777",
		"89204827777":       "+79204827777",
		"79204827777":       "+79204827777",
		"9204827777":        "+79204827777",
		"+995 555 123 456":  "+995555123456",
	}
	for in, want := range ok {
		got, err := NormalizePhone(in)
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", "12345", "abc", "+123"} {
		if got, err := NormalizePhone(in); err == nil {
			t.Errorf("NormalizePhone(%q) = %q; want error", in, got)
		}
	}
}

func TestParseAddress(t *testing.T) {
	cases := map[string]DeliveryAddress{
		"Осканова, 21":               {"Осканова", "21", ""},
		"ул. Осканова, д. 21, кв. 5": {"Осканова", "21", "5"},
		"улица Осканова 21 кв 5":     {"Осканова", "21", "5"},
		"Осканова 21":                {"Осканова", "21", ""},
		"Зязикова, 12/2, 7":          {"Зязикова", "12/2", "7"},
		"пр-т Идриса Зязикова 8а":    {"Идриса Зязикова", "8а", ""},
	}
	for in, want := range cases {
		got, err := ParseAddress(in)
		if err != nil || got != want {
			t.Errorf("ParseAddress(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "Осканова", "ул. Осканова, центр"} {
		if got, err := ParseAddress(in); err == nil {
			t.Errorf("ParseAddress(%q) = %+v; want error", in, got)
		}
	}
}

func TestOrderValidate(t *testing.T) {
	items := []OrderItem{{ProductID: "id", Price: 100, Amount: 2}}

	pickup := &Order{Phone: "89204827777", DeliveryType: DeliveryPickUp, Items: items}
	if err := pickup.Validate(); err != nil || pickup.Phone != "+79204827777" {
		t.Errorf("самовывоз: err=%v phone=%q", err, pickup.Phone)
	}

	noAddr := &Order{Phone: "89204827777", DeliveryType: DeliveryByCourier, Items: items}
	if err := noAddr.Validate(); err == nil {
		t.Error("доставка без адреса должна быть отклонена")
	}

	if err := (&Order{Phone: "89204827777", DeliveryType: DeliveryPickUp}).Validate(); err == nil {
		t.Error("пустой заказ должен быть отклонён")
	}

	bad := &Order{Phone: "89204827777", DeliveryType: DeliveryPickUp, Items: []OrderItem{{ProductID: "id", Amount: 0}}}
	if err := bad.Validate(); err == nil {
		t.Error("нулевое количество должно быть отклонено")
	}

	if got := (&Order{Items: items}).TotalSum(); got != 200 {
		t.Errorf("TotalSum = %v; want 200", got)
	}

	// Некорректный DeliveryType
	invalidDT := &Order{Phone: "89204827777", DeliveryType: "drone", Items: items}
	if err := invalidDT.Validate(); err == nil {
		t.Error("недопустимый deliveryType должен быть отклонен")
	}

	// Слишком длинное имя
	longName := &Order{
		CustomerName: strings.Repeat("а", 101),
		Phone:        "89204827777",
		DeliveryType: DeliveryPickUp,
		Items:        items,
	}
	if err := longName.Validate(); err == nil {
		t.Error("слишком длинное имя должно быть отклонено")
	}
}

func TestResolveAndVerifyPrices_ServerCalculatesPrices(t *testing.T) {
	cat := catalog.DefaultBeiniCatalog()

	// Клиент шлет поддельные цену (1 руб) и поддельное название ("Халява")
	order := &Order{
		CustomerName: "Хакер",
		Phone:        "+79204827777",
		DeliveryType: DeliveryPickUp,
		Items: []OrderItem{
			{
				ProductID: "beini-grill-01",
				Name:      "Поддельное имя",
				Price:     1.0, // Хакер хочет купить за 1 рубль
				Amount:    2,
			},
		},
	}

	err := order.ResolveAndVerifyPrices(cat)
	if err != nil {
		t.Fatalf("ResolveAndVerifyPrices вернул ошибку: %v", err)
	}

	// Проверяем, что цена и имя переписаны сервером:
	// Люля-кебаб стоит 590 руб
	item := order.Items[0]
	if item.Price != 590 {
		t.Errorf("сервер не перезаписал цену: got %v, want 590", item.Price)
	}
	if item.Name != "Люля-кебаб из телятины" {
		t.Errorf("сервер не перезаписал название: got %q", item.Name)
	}
	if item.ProductID != "0ce65922-6a9f-453a-bb92-b7c35b4950bb" {
		t.Errorf("ProductID не заменен на проверенный IikoID: got %q", item.ProductID)
	}

	// Итоговая сумма должна быть 590 * 2 = 1180 руб, а не 2 рубля!
	if total := order.TotalSum(); total != 1180 {
		t.Errorf("TotalSum = %v; want 1180", total)
	}
}

func TestResolveAndVerifyPrices_UnknownProduct(t *testing.T) {
	cat := catalog.DefaultBeiniCatalog()

	order := &Order{
		Items: []OrderItem{
			{ProductID: "unknown-dish-id-999", Amount: 1},
		},
	}

	err := order.ResolveAndVerifyPrices(cat)
	if err == nil {
		t.Fatal("неизвестный товар должен приводить к ошибке")
	}
}

func TestResolveAndVerifyPrices_ExcessiveAmount(t *testing.T) {
	cat := catalog.DefaultBeiniCatalog()

	order := &Order{
		Items: []OrderItem{
			{ProductID: "beini-grill-01", Amount: 51}, // Превышает лимит 50
		},
	}

	err := order.ResolveAndVerifyPrices(cat)
	if err == nil {
		t.Fatal("количество > 50 должно отклоняться")
	}
}

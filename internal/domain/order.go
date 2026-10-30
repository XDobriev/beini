package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"beini-backend/internal/catalog"
)

// DeliveryType определяет способ получения заказа
type DeliveryType string

const (
	DeliveryByCourier DeliveryType = "delivery"
	DeliveryPickUp    DeliveryType = "pickup"
)

// Константы ограничений для защиты от переполнения и некорректных данных
const (
	MaxOrderPositions  = 50
	MaxAmountPerItem   = 50
	MaxTotalAmount     = 200
	MaxCustomerNameLen = 100
	MaxCommentLen      = 500
	MaxAddressLen      = 200
)

// OrderItem представляет одну позицию блюда в заказе
type OrderItem struct {
	ProductID string  `json:"productId"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Amount    int     `json:"amount"`
}

// Order — доменная сущность заказа от гостя ресторана
type Order struct {
	CustomerName string       `json:"customerName"`
	Phone        string       `json:"phone"`
	DeliveryType DeliveryType `json:"deliveryType"` // "delivery" или "pickup"
	Address      string       `json:"address"`
	Comment      string       `json:"comment"`
	Items        []OrderItem  `json:"items"`
}

// DeliveryAddress — адрес доставки, разобранный на части для iiko (тип legacy)
type DeliveryAddress struct {
	Street string
	House  string
	Flat   string
}

// TotalSum рассчитывает общую сумму заказа исключительно на основе серверных цен позиций
func (o *Order) TotalSum() float64 {
	var total float64
	for _, item := range o.Items {
		total += item.Price * float64(item.Amount)
	}
	return total
}

// ResolveAndVerifyPrices сверяет каждую позицию заказа с серверным каталогом.
// Ни при каких условиях не доверяет price, name или сумме, присланным браузером.
// Все цены и наименования берутся строго из доверенного каталога.
func (o *Order) ResolveAndVerifyPrices(cat catalog.Catalog) error {
	if cat == nil {
		return errors.New("серверный каталог блюд не инициализирован")
	}

	if len(o.Items) == 0 {
		return errors.New("заказ не может быть пустым (требуется хотя бы одно блюдо)")
	}
	if len(o.Items) > MaxOrderPositions {
		return fmt.Errorf("превышено максимальное количество позиций в заказе (максимум %d)", MaxOrderPositions)
	}

	totalAmount := 0
	for i := range o.Items {
		item := &o.Items[i]
		cleanID := strings.TrimSpace(item.ProductID)
		if cleanID == "" {
			return errors.New("у каждого блюда должен быть указан productId")
		}
		if item.Amount <= 0 {
			return errors.New("количество каждого блюда должно быть больше 0")
		}
		if item.Amount > MaxAmountPerItem {
			return fmt.Errorf("количество блюда не может превышать %d порций", MaxAmountPerItem)
		}
		totalAmount += item.Amount

		product, ok := cat.FindProduct(cleanID)
		if !ok {
			return fmt.Errorf("блюдо с идентификатором %q не найдено в меню ресторана", cleanID)
		}

		// Принудительно перезаписываем поля доверенными данными из серверного каталога:
		// Браузерный price и name полностью игнорируются!
		item.ProductID = product.IikoID
		item.Name = product.Name
		item.Price = product.Price
	}

	if totalAmount > MaxTotalAmount {
		return fmt.Errorf("превышено суммарное количество порций в заказе (максимум %d)", MaxTotalAmount)
	}

	return nil
}

// Validate проверяет корректность всех полей заказа и нормализует телефон
func (o *Order) Validate() error {
	phone, err := NormalizePhone(o.Phone)
	if err != nil {
		return err
	}
	o.Phone = phone

	o.CustomerName = strings.TrimSpace(o.CustomerName)
	if len([]rune(o.CustomerName)) > MaxCustomerNameLen {
		return fmt.Errorf("имя клиента не должно превышать %d символов", MaxCustomerNameLen)
	}
	if o.CustomerName == "" {
		o.CustomerName = "Гость с сайта"
	}

	o.Comment = strings.TrimSpace(o.Comment)
	if len([]rune(o.Comment)) > MaxCommentLen {
		return fmt.Errorf("длина комментария не должна превышать %d символов", MaxCommentLen)
	}

	if o.DeliveryType != DeliveryByCourier && o.DeliveryType != DeliveryPickUp {
		return errors.New("недопустимый способ получения заказа (выберите доставку или самовывоз)")
	}

	if len(o.Items) == 0 {
		return errors.New("заказ не может быть пустым (требуется хотя бы одно блюдо)")
	}
	if len(o.Items) > MaxOrderPositions {
		return fmt.Errorf("превышено максимальное количество позиций в заказе (максимум %d)", MaxOrderPositions)
	}

	for _, item := range o.Items {
		if strings.TrimSpace(item.ProductID) == "" {
			return errors.New("у каждого блюда должен быть указан productId номенклатуры iiko")
		}
		if item.Amount <= 0 {
			return errors.New("количество каждого блюда должно быть больше 0")
		}
		if item.Amount > MaxAmountPerItem {
			return fmt.Errorf("количество одного блюда не должно превышать %d", MaxAmountPerItem)
		}
		if item.Price < 0 {
			return errors.New("цена блюда не может быть отрицательной")
		}
	}

	if o.DeliveryType == DeliveryByCourier {
		o.Address = strings.TrimSpace(o.Address)
		if len([]rune(o.Address)) > MaxAddressLen {
			return fmt.Errorf("адрес доставки не должен превышать %d символов", MaxAddressLen)
		}
		if _, err := ParseAddress(o.Address); err != nil {
			return err
		}
	}

	return nil
}

var nonDigits = regexp.MustCompile(`\D`)

// NormalizePhone приводит телефон к формату iiko: начинается с «+», минимум 8 цифр.
// Российские номера вида 8920…, 7920…, 920… превращаются в +7920…
func NormalizePhone(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("номер телефона обязателен для оформления заказа")
	}

	digits := nonDigits.ReplaceAllString(raw, "")
	switch {
	case len(digits) == 11 && (digits[0] == '8' || digits[0] == '7'):
		return "+7" + digits[1:], nil
	case len(digits) == 10:
		return "+7" + digits, nil
	case strings.HasPrefix(raw, "+") && len(digits) >= 8 && len(digits) <= 15:
		return "+" + digits, nil
	}
	return "", errors.New("укажите телефон в формате +7 920 482-77-77")
}

var (
	streetPrefix = regexp.MustCompile(`(?i)^(ул\.?|улица|пр-т\.?|проспект|пер\.?|переулок)\s+`)
	housePrefix  = regexp.MustCompile(`(?i)^(д\.?|дом)\s*`)
	flatPrefix   = regexp.MustCompile(`(?i)^(кв\.?|квартира)\s*`)
	// «Осканова 21», «Осканова 21 кв 5», «Осканова д. 21/2, кв. 5»
	addressInline = regexp.MustCompile(`(?i)^(.+?)[,\s]+(?:д\.?\s*|дом\s*)?(\d+\s?[а-яa-z]?(?:/\d+)?)(?:[,\s]+(?:кв\.?|квартира)?\s*(\d+))?$`)
)

// ParseAddress разбирает строку «улица, дом, квартира» на части.
// iiko требует для адреса доставки улицу и номер дома.
func ParseAddress(raw string) (DeliveryAddress, error) {
	raw = strings.TrimSpace(raw)
	errNoHouse := errors.New("для доставки укажите улицу и номер дома, например: Осканова, 21, кв. 5")
	if raw == "" {
		return DeliveryAddress{}, errNoHouse
	}

	var addr DeliveryAddress
	parts := strings.Split(raw, ",")
	if len(parts) >= 2 && housePrefix.ReplaceAllString(strings.TrimSpace(parts[1]), "") != "" &&
		startsWithDigit(housePrefix.ReplaceAllString(strings.TrimSpace(parts[1]), "")) {
		addr.Street = strings.TrimSpace(parts[0])
		addr.House = housePrefix.ReplaceAllString(strings.TrimSpace(parts[1]), "")
		if len(parts) >= 3 {
			addr.Flat = flatPrefix.ReplaceAllString(strings.TrimSpace(parts[2]), "")
		}
	} else if m := addressInline.FindStringSubmatch(raw); m != nil {
		addr.Street = strings.TrimSpace(m[1])
		addr.House = strings.ReplaceAll(strings.TrimSpace(m[2]), " ", "")
		addr.Flat = m[3]
	} else {
		return DeliveryAddress{}, errNoHouse
	}

	addr.Street = strings.TrimSpace(streetPrefix.ReplaceAllString(addr.Street, ""))
	if addr.Street == "" || addr.House == "" {
		return DeliveryAddress{}, errNoHouse
	}
	return addr, nil
}

func startsWithDigit(s string) bool {
	return s != "" && s[0] >= '0' && s[0] <= '9'
}

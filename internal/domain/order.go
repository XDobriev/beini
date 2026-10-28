package domain

import (
	"errors"
	"regexp"
	"strings"
)

// DeliveryType определяет способ получения заказа
type DeliveryType string

const (
	DeliveryByCourier DeliveryType = "delivery"
	DeliveryPickUp    DeliveryType = "pickup"
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

// TotalSum рассчитывает общую сумму заказа
func (o *Order) TotalSum() float64 {
	var total float64
	for _, item := range o.Items {
		total += item.Price * float64(item.Amount)
	}
	return total
}

// Validate проверяет корректность входных данных заказа и нормализует телефон
func (o *Order) Validate() error {
	phone, err := NormalizePhone(o.Phone)
	if err != nil {
		return err
	}
	o.Phone = phone

	if len(o.Items) == 0 {
		return errors.New("заказ не может быть пустым (требуется хотя бы одно блюдо)")
	}

	for _, item := range o.Items {
		if item.ProductID == "" {
			return errors.New("у каждого блюда должен быть указан productId номенклатуры iiko")
		}
		if item.Amount <= 0 {
			return errors.New("количество каждого блюда должно быть больше 0")
		}
	}

	if o.DeliveryType == DeliveryByCourier {
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

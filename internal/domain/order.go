package domain

import (
	"errors"
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

// TotalSum рассчитывает общую сумму заказа
func (o *Order) TotalSum() float64 {
	var total float64
	for _, item := range o.Items {
		total += item.Price * float64(item.Amount)
	}
	return total
}

// Validate проверяет корректность входных данных заказа
func (o *Order) Validate() error {
	o.Phone = strings.TrimSpace(o.Phone)
	if o.Phone == "" {
		return errors.New("номер телефона обязателен для оформления заказа")
	}

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
		if strings.TrimSpace(o.Address) == "" {
			return errors.New("для курьерской доставки обязательно укажите адрес")
		}
	}

	return nil
}


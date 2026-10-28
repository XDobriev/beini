package iiko

// AccessTokenRequest тело запроса для /api/v2/access_token
type AccessTokenRequest struct {
	APILogin     string `json:"apiLogin"`
	AppID        string `json:"appId"`
	ClientSecret string `json:"clientSecret"`
}

// AccessTokenResponse ответ от /api/v2/access_token
type AccessTokenResponse struct {
	Token string `json:"token"`
}

// IikoCustomer описание гостя в заказе
type IikoCustomer struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// IikoStreet описание улицы в адресе доставки
type IikoStreet struct {
	Name string `json:"name"`
}

// IikoAddress адрес точки доставки
type IikoAddress struct {
	Street IikoStreet `json:"street"`
}

// IikoDeliveryPoint точка доставки заказа
type IikoDeliveryPoint struct {
	Address IikoAddress `json:"address"`
}

// IikoOrderItem позиция номенклатуры в заказе
type IikoOrderItem struct {
	ProductID string  `json:"productId"`
	Price     float64 `json:"price"`
	Type      string  `json:"type"` // "Product"
	Amount    int     `json:"amount"`
}

// IikoPayment информация об оплате
type IikoPayment struct {
	PaymentTypeKind string  `json:"paymentTypeKind"` // "Cash"
	Sum             float64 `json:"sum"`
	PaymentTypeID   string  `json:"paymentTypeId"`
	IsPaid          bool    `json:"isPaid"`
}

// IikoOrder внутренняя структура заказа iiko
type IikoOrder struct {
	Phone         string             `json:"phone"`
	OrderTypeID   string             `json:"orderTypeId"`
	Customer      IikoCustomer       `json:"customer"`
	DeliveryPoint *IikoDeliveryPoint `json:"deliveryPoint,omitempty"`
	Items         []IikoOrderItem    `json:"items"`
	Payments      []IikoPayment      `json:"payments"`
	Comment       string             `json:"comment"`
}

// CreateDeliveryRequest запрос к /api/1/deliveries/create
type CreateDeliveryRequest struct {
	OrganizationID  string    `json:"organizationId"`
	TerminalGroupID string    `json:"terminalGroupId"`
	Order           IikoOrder `json:"order"`
}

// ErrorInfo описание ошибки создания заказа на кассовом терминале
type ErrorInfo struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Description string `json:"description"`
}

// OrderInfo статус заказа в iikoCloud
type OrderInfo struct {
	ID             string     `json:"id"`
	CreationStatus string     `json:"creationStatus"` // "Success", "InProgress", "Error"
	ErrorInfo      *ErrorInfo `json:"errorInfo,omitempty"`
}

// CreateDeliveryResponse ответ от /api/1/deliveries/create
type CreateDeliveryResponse struct {
	CorrelationID    string    `json:"correlationId"`
	OrderInfo        OrderInfo `json:"orderInfo"`
	ErrorDescription string    `json:"errorDescription,omitempty"`
}


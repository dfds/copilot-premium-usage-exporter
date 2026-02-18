package github

type SeatsResponse struct {
	TotalSeats int           `json:"total_seats"`
	Seats      []CopilotSeat `json:"seats"`
}

type CopilotSeat struct {
	Assignee Assignee `json:"assignee"`
}

type Assignee struct {
	Login string `json:"login"`
}

type UsageResponse struct {
	Enterprise string      `json:"enterprise"`
	User       string      `json:"user"`
	UsageItems []UsageItem `json:"usageItems"`
}

type UsageItem struct {
	Product          string  `json:"product"`
	SKU              string  `json:"sku"`
	Model            string  `json:"model"`
	UnitType         string  `json:"unitType"`
	PricePerUnit     float64 `json:"pricePerUnit"`
	GrossQuantity    float64 `json:"grossQuantity"`
	GrossAmount      float64 `json:"grossAmount"`
	DiscountQuantity float64 `json:"discountQuantity"`
	DiscountAmount   float64 `json:"discountAmount"`
	NetQuantity      float64 `json:"netQuantity"`
	NetAmount        float64 `json:"netAmount"`
}

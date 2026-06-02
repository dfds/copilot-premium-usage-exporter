package github

type BillingUsageResponse struct {
	UsageItems []BillingUsageItem `json:"usageItems"`
}

type BillingUsageItem struct {
	Date             string  `json:"date"`
	Product          string  `json:"product"`
	SKU              string  `json:"sku"`
	Quantity         float64 `json:"quantity"`
	UnitType         string  `json:"unitType"`
	PricePerUnit     float64 `json:"pricePerUnit"`
	GrossAmount      float64 `json:"grossAmount"`
	DiscountAmount   float64 `json:"discountAmount"`
	NetAmount        float64 `json:"netAmount"`
	OrganizationName string  `json:"organizationName"`
	RepositoryName   string  `json:"repositoryName"`
}

type BillingReportCreateRequest struct {
	ReportType string `json:"report_type"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date,omitempty"`
	SendEmail  bool   `json:"send_email"`
}

type BillingReportStatus struct {
	ID           string   `json:"id"`
	ReportType   string   `json:"report_type"`
	StartDate    string   `json:"start_date"`
	EndDate      string   `json:"end_date"`
	Status       string   `json:"status"`
	DownloadURLs []string `json:"download_urls"`
	CreatedAt    string   `json:"created_at"`
	Actor        string   `json:"actor"`
}

// AICreditRow is one row of the ai_credit CSV report — a per-user × model × day
// breakdown of AI Credit consumption and cost. The report supersedes the
// retired premium_request endpoint as of 2026-06-01.
type AICreditRow struct {
	Date           string
	Username       string
	Product        string
	SKU            string
	Model          string
	Quantity       float64
	UnitType       string
	GrossAmount    float64
	DiscountAmount float64
	NetAmount      float64
	Organization   string
	Repository     string
	CostCenterName string
}

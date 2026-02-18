package internal

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var labels = []string{"user", "sku", "model", "enterprise"}

var RequestAmount *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_user_usage_request_amount",
	Help: "Number of Copilot premium requests per user, SKU, and model for the current month",
}, labels)

var RequestCostGross *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_user_usage_request_cost_gross",
	Help: "Gross cost in USD of Copilot premium requests per user, SKU, and model for the current month",
}, labels)

var RequestCostDiscount *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_user_usage_request_cost_discount",
	Help: "Discount amount in USD applied to Copilot premium requests per user, SKU, and model for the current month",
}, labels)

package internal

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Per-user Copilot AI Credit metrics sourced from the asynchronous ai_credit
// billing report. Premium Request Units were retired 2026-06-01; AI Credits
// are the replacement billing unit. The report is requested for a month-to-date
// window (1st of month through the most recently settled day); gauges hold the
// running month-to-date total per tuple and step down when the month rolls over.
var userAICreditLabels = []string{"user", "sku", "model", "organization", "cost_center", "enterprise"}

var UserAICreditQuantity *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_user_ai_credit_quantity",
	Help: "Copilot AI Credits consumed per user, SKU, model, organization, and cost center, month-to-date",
}, userAICreditLabels)

var UserAICreditGrossAmount *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_user_ai_credit_gross_amount_usd",
	Help: "Gross Copilot AI Credit cost in USD per user, SKU, model, organization, and cost center, month-to-date",
}, userAICreditLabels)

var UserAICreditDiscountAmount *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_user_ai_credit_discount_amount_usd",
	Help: "Discount applied in USD per user, SKU, model, organization, and cost center, month-to-date",
}, userAICreditLabels)

var UserAICreditNetAmount *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_user_ai_credit_net_amount_usd",
	Help: "Net Copilot AI Credit cost in USD (gross minus discount) per user, SKU, model, organization, and cost center, month-to-date",
}, userAICreditLabels)

// Enterprise-level Copilot billing metrics sourced from the aggregate
// /settings/billing/usage endpoint. No per-user breakdown is available.
var billingLabels = []string{"sku", "unit_type", "organization", "repository", "enterprise"}

var BillingQuantity *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_billing_quantity",
	Help: "Copilot billing usage quantity for the most recent month, in the unit_type label's unit (Requests, AICredits, UserMonths, etc.)",
}, billingLabels)

var BillingGrossAmount *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_billing_gross_amount_usd",
	Help: "Gross Copilot billing amount in USD for the most recent month",
}, billingLabels)

var BillingDiscountAmount *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_billing_discount_amount_usd",
	Help: "Discount applied to Copilot billing in USD for the most recent month",
}, billingLabels)

var BillingNetAmount *prometheus.GaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "github_copilot_billing_net_amount_usd",
	Help: "Net Copilot billing amount in USD (gross minus discount) for the most recent month",
}, billingLabels)

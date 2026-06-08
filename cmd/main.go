package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	bootstraplog "go.dfds.cloud/bootstrap/log"
	"go.dfds.cloud/copilot-premium-usage-exporter/internal"
	"go.dfds.cloud/copilot-premium-usage-exporter/internal/config"
	"go.dfds.cloud/copilot-premium-usage-exporter/internal/github"
	"go.uber.org/zap"
)

var logger *zap.Logger
var collectMu sync.RWMutex

type userEntry struct {
	labels         prometheus.Labels
	quantity       float64
	grossAmount    float64
	discountAmount float64
	netAmount      float64
}

type billingEntry struct {
	labels         prometheus.Labels
	quantity       float64
	grossAmount    float64
	discountAmount float64
	netAmount      float64
}

func main() {
	conf, err := config.Load()
	if err != nil {
		panic(err)
	}

	bootstraplog.InitializeLogger(conf.LogDebug, conf.LogLevel)
	logger = bootstraplog.Logger
	defer logger.Sync()

	logger.Info("starting copilot-premium-usage-exporter")

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(pprof.New())
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		collectMu.RLock()
		defer collectMu.RUnlock()
		promhttp.Handler().ServeHTTP(w, r)
	})
	app.Get("/metrics", adaptor.HTTPHandler(metricsHandler))

	go worker(conf)

	if err := app.Listen(":8080"); err != nil {
		panic(err)
	}
}

func worker(conf config.Config) {
	sleepInterval := time.Duration(conf.WorkerInterval) * time.Second
	client := github.NewClient(conf.Github.Token, logger)

	for {
		logger.Info("collecting copilot usage metrics")

		if err := collect(client, conf.Github.Enterprise, conf.ReportLagDays); err != nil {
			logger.Error("failed to collect metrics", zap.Error(err))
		} else {
			logger.Info("metrics published")
		}

		time.Sleep(sleepInterval)
	}
}

func collect(client *github.Client, enterprise string, reportLagDays int) error {
	userEntries, err := collectUserAICredits(client, enterprise, reportLagDays)
	if err != nil {
		return fmt.Errorf("collecting per-user ai_credit report: %w", err)
	}

	billingEntries, err := collectAggregateBilling(client, enterprise)
	if err != nil {
		return fmt.Errorf("collecting aggregate billing usage: %w", err)
	}

	collectMu.Lock()
	defer collectMu.Unlock()

	internal.UserAICreditQuantity.Reset()
	internal.UserAICreditGrossAmount.Reset()
	internal.UserAICreditDiscountAmount.Reset()
	internal.UserAICreditNetAmount.Reset()
	internal.BillingQuantity.Reset()
	internal.BillingGrossAmount.Reset()
	internal.BillingDiscountAmount.Reset()
	internal.BillingNetAmount.Reset()

	for _, e := range userEntries {
		internal.UserAICreditQuantity.With(e.labels).Set(e.quantity)
		internal.UserAICreditGrossAmount.With(e.labels).Set(e.grossAmount)
		internal.UserAICreditDiscountAmount.With(e.labels).Set(e.discountAmount)
		internal.UserAICreditNetAmount.With(e.labels).Set(e.netAmount)
	}
	for _, e := range billingEntries {
		internal.BillingQuantity.With(e.labels).Set(e.quantity)
		internal.BillingGrossAmount.With(e.labels).Set(e.grossAmount)
		internal.BillingDiscountAmount.With(e.labels).Set(e.discountAmount)
		internal.BillingNetAmount.With(e.labels).Set(e.netAmount)
	}

	return nil
}

func collectUserAICredits(client *github.Client, enterprise string, lagDays int) ([]userEntry, error) {
	// Month-to-date window: the 1st of the month containing the most recently
	// settled day (today - lagDays) through that day. Anchoring the month on
	// the end day handles the start-of-month case — e.g. on the 1st with
	// lagDays=1 the window covers the whole previous (now-complete) month until
	// the new month starts to accrue settled days.
	end := time.Now().UTC().AddDate(0, 0, -lagDays)
	start := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
	startDate := start.Format("2006-01-02")
	endDate := end.Format("2006-01-02")
	logger.Info("requesting ai_credit billing report",
		zap.String("start", startDate),
		zap.String("end", endDate),
	)

	urls, err := client.CreateAndAwaitBillingReport(enterprise, "ai_credit", startDate, endDate)
	if err != nil {
		return nil, err
	}

	rows, err := client.FetchAICreditRows(urls)
	if err != nil {
		return nil, fmt.Errorf("downloading ai_credit csv: %w", err)
	}
	logger.Info("fetched ai_credit rows",
		zap.Int("rows", len(rows)),
		zap.String("start", startDate),
		zap.String("end", endDate),
	)

	// One (user, sku, model, org, cost_center) tuple spans many CSV rows across
	// the month-to-date window (one per day, and per repository within a day);
	// summing them yields the running month-to-date total per tuple so Set()
	// writes a single cumulative value instead of dropping earlier rows.
	type key struct{ user, sku, model, org, costCenter string }
	agg := make(map[key]*userEntry)
	for _, r := range rows {
		k := key{r.Username, r.SKU, r.Model, r.Organization, r.CostCenterName}
		e, ok := agg[k]
		if !ok {
			e = &userEntry{
				labels: prometheus.Labels{
					"user":         r.Username,
					"sku":          r.SKU,
					"model":        r.Model,
					"organization": r.Organization,
					"cost_center":  r.CostCenterName,
					"enterprise":   enterprise,
				},
			}
			agg[k] = e
		}
		e.quantity += r.Quantity
		e.grossAmount += r.GrossAmount
		e.discountAmount += r.DiscountAmount
		e.netAmount += r.NetAmount
	}

	out := make([]userEntry, 0, len(agg))
	for _, e := range agg {
		out = append(out, *e)
	}
	return out, nil
}

func collectAggregateBilling(client *github.Client, enterprise string) ([]billingEntry, error) {
	billing, err := client.GetEnterpriseBillingUsage(enterprise)
	if err != nil {
		return nil, err
	}
	latestYear, latestMonth := mostRecentMonth(billing.UsageItems)
	if latestYear == 0 {
		return nil, nil
	}

	var entries []billingEntry
	for _, item := range billing.UsageItems {
		if item.Product != "copilot" {
			continue
		}
		y, m, ok := parseBillingDate(item.Date)
		if !ok || y != latestYear || m != latestMonth {
			continue
		}
		entries = append(entries, billingEntry{
			labels: prometheus.Labels{
				"sku":          item.SKU,
				"unit_type":    item.UnitType,
				"organization": item.OrganizationName,
				"repository":   item.RepositoryName,
				"enterprise":   enterprise,
			},
			quantity:       item.Quantity,
			grossAmount:    item.GrossAmount,
			discountAmount: item.DiscountAmount,
			netAmount:      item.NetAmount,
		})
	}
	return entries, nil
}

func parseBillingDate(s string) (year, month int, ok bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0, 0, false
	}
	return t.Year(), int(t.Month()), true
}

// mostRecentMonth returns the (year, month) of the newest UsageItem.Date in the
// report. Returns 0, 0 when the report is empty or no date parses.
func mostRecentMonth(items []github.BillingUsageItem) (int, int) {
	var bestY, bestM int
	for _, it := range items {
		y, m, ok := parseBillingDate(it.Date)
		if !ok {
			continue
		}
		if y > bestY || (y == bestY && m > bestM) {
			bestY, bestM = y, m
		}
	}
	return bestY, bestM
}

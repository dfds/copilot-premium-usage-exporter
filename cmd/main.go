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

type metricEntry struct {
	labels         prometheus.Labels
	grossQuantity  float64
	grossAmount    float64
	discountAmount float64
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
		logger.Info("collecting copilot premium usage metrics")

		if err := collect(client, conf.Github.Enterprise); err != nil {
			logger.Error("failed to collect metrics", zap.Error(err))
		} else {
			logger.Info("metrics published")
		}

		time.Sleep(sleepInterval)
	}
}

func collect(client *github.Client, enterprise string) error {
	logins, err := client.ListCopilotSeats(enterprise)
	if err != nil {
		return fmt.Errorf("listing copilot seats: %w", err)
	}

	logger.Info("found copilot seat holders", zap.Int("count", len(logins)))

	var entries []metricEntry
	for _, login := range logins {
		usage, err := client.GetUserPremiumUsage(enterprise, login)
		if err != nil {
			logger.Warn("failed to get usage for user", zap.String("user", login), zap.Error(err))
			continue
		}

		for _, item := range usage.UsageItems {
			entries = append(entries, metricEntry{
				labels: prometheus.Labels{
					"user":       login,
					"sku":        item.SKU,
					"model":      item.Model,
					"enterprise": enterprise,
				},
				grossQuantity:  item.GrossQuantity,
				grossAmount:    item.GrossAmount,
				discountAmount: item.DiscountAmount,
			})
		}
	}

	collectMu.Lock()
	defer collectMu.Unlock()

	internal.RequestAmount.Reset()
	internal.RequestCostGross.Reset()
	internal.RequestCostDiscount.Reset()

	for _, e := range entries {
		internal.RequestAmount.With(e.labels).Set(e.grossQuantity)
		internal.RequestCostGross.With(e.labels).Set(e.grossAmount)
		internal.RequestCostDiscount.With(e.labels).Set(e.discountAmount)
	}

	return nil
}

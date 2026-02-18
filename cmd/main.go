package main

import (
	"fmt"
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
	app.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))

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

	internal.RequestAmount.Reset()
	internal.RequestCostGross.Reset()
	internal.RequestCostDiscount.Reset()

	for _, login := range logins {
		usage, err := client.GetUserPremiumUsage(enterprise, login)
		if err != nil {
			logger.Warn("failed to get usage for user", zap.String("user", login), zap.Error(err))
			continue
		}

		for _, item := range usage.UsageItems {
			lbls := prometheus.Labels{
				"user":       login,
				"sku":        item.SKU,
				"model":      item.Model,
				"enterprise": enterprise,
			}
			internal.RequestAmount.With(lbls).Set(item.GrossQuantity)
			internal.RequestCostGross.With(lbls).Set(item.GrossAmount)
			internal.RequestCostDiscount.With(lbls).Set(item.DiscountAmount)
		}
	}

	return nil
}

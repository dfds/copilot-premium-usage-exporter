package config

import "github.com/kelseyhightower/envconfig"

type Config struct {
	LogLevel       string `json:"logLevel"`
	LogDebug       bool   `json:"logDebug"`
	WorkerInterval int    `json:"workerInterval"`
	// ReportLagDays controls which UTC day the ai_credit billing report is
	// requested for: day = today - ReportLagDays. Default 1 (yesterday) so
	// the report has had a full day to settle before we publish it.
	ReportLagDays int `json:"reportLagDays"`
	Github        struct {
		Token      string `json:"token"`
		Enterprise string `json:"enterprise"`
	} `json:"github"`
}

const appConfPrefix = "CPUE"

func Load() (Config, error) {
	var conf Config
	err := envconfig.Process(appConfPrefix, &conf)

	if conf.LogLevel == "" {
		conf.LogLevel = "info"
	}
	if conf.WorkerInterval == 0 {
		conf.WorkerInterval = 3600
	}
	if conf.ReportLagDays == 0 {
		conf.ReportLagDays = 1
	}

	return conf, err
}

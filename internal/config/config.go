package config

import "github.com/kelseyhightower/envconfig"

type Config struct {
	LogLevel       string `json:"logLevel" default:"info"`
	LogDebug       bool   `json:"logDebug"`
	WorkerInterval int    `json:"workerInterval" default:"3600"`
	// ReportLagDays controls which UTC day the ai_credit billing report is
	// requested for: day = today - ReportLagDays. Default 1 (yesterday) so
	// the report has had a full day to settle before we publish it. An
	// explicit 0 requests today (incomplete, self-correcting across hourly
	// scrapes) — the default tag preserves it, unlike a zero-value check.
	ReportLagDays int `json:"reportLagDays" default:"1"`
	Github        struct {
		Token      string `json:"token"`
		Enterprise string `json:"enterprise"`
	} `json:"github"`
}

const appConfPrefix = "CPUE"

func Load() (Config, error) {
	var conf Config
	err := envconfig.Process(appConfPrefix, &conf)
	return conf, err
}

package config

import "github.com/kelseyhightower/envconfig"

type Config struct {
	WorkerInterval int `json:"workerInterval"`
	Github         struct {
		Token      string `json:"token"`
		Enterprise string `json:"enterprise"`
	} `json:"github"`
}

const appConfPrefix = "CPUE"

func Load() (Config, error) {
	var conf Config
	err := envconfig.Process(appConfPrefix, &conf)

	if conf.WorkerInterval == 0 {
		conf.WorkerInterval = 3600
	}

	return conf, err
}

// config/config.go
package config

import (
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	Bybit BybitConfig `mapstructure:"bybit"`
	Order OrderConfig `mapstructure:"order"`
}

type BybitConfig struct {
	WSUrl  string `mapstructure:"ws_url"`
	Symbol string `mapstructure:"symbol"`
}

type OrderConfig struct {
	Quantity int64 `mapstructure:"quantity"`
}

func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	log.Printf("Config loaded: %+v", cfg)
	return &cfg, nil
}


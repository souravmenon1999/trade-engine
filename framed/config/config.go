package config

import (
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	BybitOrderbook BybitOrderbookConfig `mapstructure:"bybitOrderbook"`
	Order          OrderConfig          `mapstructure:"order"`
}

type BybitOrderbookConfig struct {
	WSUrl         string `mapstructure:"ws_url"`
	Symbol        string `mapstructure:"symbol"`
	BaseCurrency  string `mapstructure:"base_currency"`
	QuoteCurrency string `mapstructure:"quote_currency"`
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
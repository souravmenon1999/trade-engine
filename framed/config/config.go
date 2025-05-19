package config

import (
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	BybitOrderbook      *BybitOrderbookConfig      `mapstructure:"bybitOrderbook"`
	BybitExchangeClient *BybitExchangeClientConfig `mapstructure:"bybitExchangeClient"`
	Order               *OrderConfig               `mapstructure:"order"`
	InjectiveExchange   *InjectiveExchangeConfig   `mapstructure:"injectiveExchange"`
}

type BybitOrderbookConfig struct {
	WSUrl         string `mapstructure:"ws_url"`
	Symbol        string `mapstructure:"symbol"`
	BaseCurrency  string `mapstructure:"base_currency"`
	QuoteCurrency string `mapstructure:"quote_currency"`
}

type BybitExchangeClientConfig struct {
	WSUrl     string `mapstructure:"ws_url"`
	APIKey    string `mapstructure:"api_key"`
	APISecret string `mapstructure:"api_secret"`
}

type OrderConfig struct {
	Quantity int64 `mapstructure:"quantity"`
}

type InjectiveExchangeConfig struct {
	NetworkName   string `mapstructure:"network_name"`
	Lb            string `mapstructure:"lb"`
	PrivKey       string `mapstructure:"priv_key"`
	MarketId      string `mapstructure:"market_id"`
	SubaccountId  string `mapstructure:"subaccount_id"`
}

func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}

	log.Printf("Config loaded: %+v", cfg)
	return cfg, nil
}
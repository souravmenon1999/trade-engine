package config

import (
	"log"
	"sync/atomic"

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
	TradingWSUrl string `mapstructure:"trading_ws_url"` // Added for trading WebSocket
	UpdatesWSUrl string `mapstructure:"updates_ws_url"` // Added for updates WebSocket
	APIKey       string `mapstructure:"api_key"`
	APISecret    string `mapstructure:"api_secret"`
}

type OrderConfig struct {
	Quantity atomic.Int64 `mapstructure:"quantity"`
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

	// Temporarily load into a non-atomic struct for unmarshaling
	type tempOrderConfig struct {
		Quantity int64 `mapstructure:"quantity"`
	}
	type tempConfig struct {
		BybitOrderbook      *BybitOrderbookConfig      `mapstructure:"bybitOrderbook"`
		BybitExchangeClient *BybitExchangeClientConfig `mapstructure:"bybitExchangeClient"`
		Order               *tempOrderConfig           `mapstructure:"order"`
		InjectiveExchange   *InjectiveExchangeConfig   `mapstructure:"injectiveExchange"`
	}

	var tempCfg tempConfig
	if err := viper.Unmarshal(&tempCfg); err != nil {
		return nil, err
	}

	// Convert to atomic types
	cfg := &Config{
		BybitOrderbook:      tempCfg.BybitOrderbook,
		BybitExchangeClient: tempCfg.BybitExchangeClient,
		Order:               &OrderConfig{},
		InjectiveExchange:   tempCfg.InjectiveExchange,
	}
	if tempCfg.Order != nil {
		cfg.Order.Quantity.Store(tempCfg.Order.Quantity)
	}

	log.Printf("Config loaded: %+v", cfg)
	return cfg, nil
}
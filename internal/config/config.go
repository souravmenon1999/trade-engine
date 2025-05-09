// internal/config/config.go
package config

import (
	
	"github.com/spf13/viper"
	"github.com/souravmenon1999/trade-engine/internal/types" // Import types for currency/exchange enums
)

// Config holds the application configuration.
type Config struct {
	Bybit     BybitConfig     `mapstructure:"bybit"`
	Injective InjectiveConfig `mapstructure:"injective"`
	Order     OrderConfig     `mapstructure:"order"`
	Processor ProcessorConfig `mapstructure:"processor"`
	Log       LogConfig       `mapstructure:"log"`
}

// BybitConfig holds Bybit specific settings.
type BybitConfig struct {
	WSURL  string `mapstructure:"ws_url"`
	Symbol string `mapstructure:"symbol"`
}

// InjectiveConfig holds Injective specific settings.
type InjectiveConfig struct {
	MarketID    string `mapstructure:"market_id"`
	PrivateKey  string `mapstructure:"private_key"`
	ChainConfig string `mapstructure:"chain_config"` // Placeholder for chain-specific settings
}

// OrderConfig holds default order parameters.
type OrderConfig struct {
	Quantity  float64 `mapstructure:"quantity"`   // Use float64 for config parsing, scale later
	SpreadPct float64 `mapstructure:"spread_pct"` // Percentage spread
}

// ProcessorConfig holds price processor settings.
type ProcessorConfig struct {
	RequoteThreshold float64 `mapstructure:"requote_threshold"` // Percentage price change
	RequoteCooldown  int     `mapstructure:"requote_cooldown"`  // Seconds
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level string `mapstructure:"level"` // debug, info, warn, error
}

// LoadConfig reads configuration from a file.
func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// Read the config file
	if err := viper.ReadInConfig(); err != nil {
		return nil, types.TradingError{
			Code:    types.ErrConfigLoading,
			Message: "Failed to read config file",
			Wrapped: err,
		}
	}

	// Unmarshal the config into the struct
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, types.TradingError{
			Code:    types.ErrConfigLoading,
			Message: "Failed to unmarshal config",
			Wrapped: err,
		}
	}

	// TODO: Add validation for config values

	return &cfg, nil
}
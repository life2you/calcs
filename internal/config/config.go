package config

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config 应用配置结构
type Config struct {
	Exchanges      ExchangesConfig      `mapstructure:"exchanges"`
	Trading        TradingConfig        `mapstructure:"trading"`
	RiskManagement RiskManagementConfig `mapstructure:"risk_management"`
	System         SystemConfig         `mapstructure:"system"`
	Redis          RedisConfig          `mapstructure:"redis"`
	Postgres       PostgresConfig       `mapstructure:"postgres"`
	Notification   NotificationConfig   `mapstructure:"notification"`
}

// ExchangesConfig 交易所配置
type ExchangesConfig struct {
	Binance BinanceConfig `mapstructure:"binance"`
	OKX     OKXConfig     `mapstructure:"okx"`
	Bitget  BitgetConfig  `mapstructure:"bitget"`
}

// BinanceConfig Binance配置
type BinanceConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	APIKey    string `mapstructure:"api_key"`    // 从配置文件中读取
	APISecret string `mapstructure:"api_secret"` // 从配置文件中读取
}

// OKXConfig OKX配置
type OKXConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	APIKey     string `mapstructure:"api_key"`    // 从配置文件中读取
	APISecret  string `mapstructure:"api_secret"` // 从配置文件中读取
	Passphrase string `mapstructure:"passphrase"` // 从配置文件中读取
}

// BitgetConfig Bitget配置
type BitgetConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	APIKey     string `mapstructure:"api_key"`    // 从配置文件中读取
	APISecret  string `mapstructure:"api_secret"` // 从配置文件中读取
	Passphrase string `mapstructure:"passphrase"` // 从配置文件中读取
}

// TradingConfig 交易配置
type TradingConfig struct {
	MinYearlyFundingRate   float64  `mapstructure:"min_yearly_funding_rate"`
	MaxLeverage            int      `mapstructure:"max_leverage"`
	MaxPositionSizePercent float64  `mapstructure:"max_position_size_percent"`
	AccountUsageLimit      float64  `mapstructure:"account_usage_limit"`
	MinProfitThreshold     float64  `mapstructure:"min_profit_threshold"`
	AllowedSymbols         []string `mapstructure:"allowed_symbols"`
}

// RiskManagementConfig 风险管理配置
type RiskManagementConfig struct {
	LiquidationWarningThreshold   float64 `mapstructure:"liquidation_warning_threshold"`
	LiquidationEmergencyThreshold float64 `mapstructure:"liquidation_emergency_threshold"`
	HedgeRebalanceThreshold       float64 `mapstructure:"hedge_rebalance_threshold"`
	MaxPositionTimeDays           int     `mapstructure:"max_position_time_days"`
	VolatilityRiskWeight          float64 `mapstructure:"volatility_risk_weight"`
	LiquidationRiskWeight         float64 `mapstructure:"liquidation_risk_weight"`
	LiquidityRiskWeight           float64 `mapstructure:"liquidity_risk_weight"`
	ExchangeRiskWeight            float64 `mapstructure:"exchange_risk_weight"`
	MaxHedgeRatioAdjustment       float64 `mapstructure:"max_hedge_ratio_adjustment"`
	AnomalyZScoreThreshold        float64 `mapstructure:"anomaly_z_score_threshold"`
}

// SystemConfig 系统配置
type SystemConfig struct {
	FundingRateCheckIntervalMinutes int    `mapstructure:"funding_rate_check_interval_minutes"`
	RiskMonitoringIntervalMinutes   int    `mapstructure:"risk_monitoring_interval_minutes"`
	LogLevel                        string `mapstructure:"log_level"`
	DataDir                         string `mapstructure:"data_dir"`
	LogDir                          string `mapstructure:"log_dir"`
}

// RedisConfig Redis配置
type RedisConfig struct {
	Host      string `mapstructure:"host"`
	Port      int    `mapstructure:"port"`
	Password  string `mapstructure:"password"`
	DB        int    `mapstructure:"db"`
	KeyPrefix string `mapstructure:"key_prefix"`
}

// PostgresConfig PostgreSQL配置
type PostgresConfig struct {
	Host           string `mapstructure:"host"`
	Port           int    `mapstructure:"port"`
	Database       string `mapstructure:"database"`
	User           string `mapstructure:"user"`
	Password       string `mapstructure:"password"` // 从配置文件或环境变量中读取
	MaxConnections int    `mapstructure:"max_connections"`
	SSLMode        string `mapstructure:"ssl_mode"`
}

// NotificationConfig 通知配置
type NotificationConfig struct {
	Telegram TelegramConfig `mapstructure:"telegram"`
}

// TelegramConfig Telegram配置
type TelegramConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	BotToken string `mapstructure:"bot_token"` // 从配置文件或环境变量中读取
	ChatID   string `mapstructure:"chat_id"`   // 从配置文件或环境变量中读取
}

// LoadConfig 从文件加载配置
func LoadConfig(filePath string) (*Config, error) {
	// 使用Viper读取配置
	v := viper.New()
	v.SetConfigFile(filePath)

	// 读取配置文件
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 绑定环境变量（可选，如果需要从环境变量覆盖配置）
	v.AutomaticEnv()
	v.SetEnvPrefix("CALCS") // 环境变量前缀，如CALCS_BINANCE_API_KEY

	// 特定环境变量映射，如果存在这些环境变量则优先使用
	if binanceApiKey := os.Getenv("BINANCE_API_KEY"); binanceApiKey != "" {
		v.Set("exchanges.binance.api_key", binanceApiKey)
	}
	if binanceApiSecret := os.Getenv("BINANCE_API_SECRET"); binanceApiSecret != "" {
		v.Set("exchanges.binance.api_secret", binanceApiSecret)
	}
	if okxApiKey := os.Getenv("OKX_API_KEY"); okxApiKey != "" {
		v.Set("exchanges.okx.api_key", okxApiKey)
	}
	if okxApiSecret := os.Getenv("OKX_API_SECRET"); okxApiSecret != "" {
		v.Set("exchanges.okx.api_secret", okxApiSecret)
	}
	if okxPassphrase := os.Getenv("OKX_PASSPHRASE"); okxPassphrase != "" {
		v.Set("exchanges.okx.passphrase", okxPassphrase)
	}

	// 解析配置到结构体
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	// 验证配置有效性
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	// 输出调试信息，查看Binance API密钥是否正确读取
	fmt.Printf("Debug - Binance配置: Enabled=%v, APIKey长度=%d, APISecret长度=%d\n",
		config.Exchanges.Binance.Enabled,
		len(config.Exchanges.Binance.APIKey),
		len(config.Exchanges.Binance.APISecret))

	return &config, nil
}

// 保留原有的yaml加载函数以备不时之需
func LoadConfigFromYAML(filePath string) (*Config, error) {
	yamlFile, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 验证配置有效性
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	return &config, nil
}

// validateConfig 验证配置有效性
func validateConfig(config *Config) error {
	// 验证至少有一个交易所启用
	if !config.Exchanges.Binance.Enabled && !config.Exchanges.OKX.Enabled && !config.Exchanges.Bitget.Enabled {
		return fmt.Errorf("至少需要启用一个交易所")
	}

	// 验证启用的交易所有API密钥
	if config.Exchanges.Binance.Enabled {
		if config.Exchanges.Binance.APIKey == "" || config.Exchanges.Binance.APISecret == "" {
			return fmt.Errorf("Binance已启用，但API密钥未配置")
		}
	}

	if config.Exchanges.OKX.Enabled {
		if config.Exchanges.OKX.APIKey == "" || config.Exchanges.OKX.APISecret == "" || config.Exchanges.OKX.Passphrase == "" {
			return fmt.Errorf("OKX已启用，但API密钥未完全配置")
		}
	}

	if config.Exchanges.Bitget.Enabled {
		if config.Exchanges.Bitget.APIKey == "" || config.Exchanges.Bitget.APISecret == "" || config.Exchanges.Bitget.Passphrase == "" {
			return fmt.Errorf("Bitget已启用，但API密钥未完全配置")
		}
	}

	// 验证交易参数
	if config.Trading.MinYearlyFundingRate <= 0 {
		return fmt.Errorf("最低年化资金费率必须大于0")
	}

	if config.Trading.MaxLeverage <= 0 {
		return fmt.Errorf("最大杠杆倍数必须大于0")
	}

	if config.Trading.MaxPositionSizePercent <= 0 || config.Trading.MaxPositionSizePercent > 1 {
		return fmt.Errorf("最大仓位大小必须在0到1之间")
	}

	if config.Trading.AccountUsageLimit <= 0 || config.Trading.AccountUsageLimit > 1 {
		return fmt.Errorf("账户使用限制必须在0到1之间")
	}

	// 验证Redis配置
	if config.Redis.Host == "" {
		return fmt.Errorf("Redis主机不能为空")
	}

	if config.Redis.Port <= 0 || config.Redis.Port > 65535 {
		return fmt.Errorf("无效的Redis端口")
	}

	return nil
}

// GetDefaultConfig 获取默认配置（用于生成示例配置）
func GetDefaultConfig() *Config {
	return &Config{
		Exchanges: ExchangesConfig{
			Binance: BinanceConfig{
				Enabled: true,
			},
			OKX: OKXConfig{
				Enabled: true,
			},
			Bitget: BitgetConfig{
				Enabled: true,
			},
		},
		Trading: TradingConfig{
			MinYearlyFundingRate:   50.0,
			MaxLeverage:            5,
			MaxPositionSizePercent: 0.6,
			AccountUsageLimit:      0.75,
			MinProfitThreshold:     0.5,
			AllowedSymbols:         []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"},
		},
		RiskManagement: RiskManagementConfig{
			LiquidationWarningThreshold:   0.25,
			LiquidationEmergencyThreshold: 0.15,
			HedgeRebalanceThreshold:       0.03,
			MaxPositionTimeDays:           14,
			VolatilityRiskWeight:          0.3,
			LiquidationRiskWeight:         0.4,
			LiquidityRiskWeight:           0.2,
			ExchangeRiskWeight:            0.1,
			MaxHedgeRatioAdjustment:       0.2,
			AnomalyZScoreThreshold:        3.0,
		},
		System: SystemConfig{
			FundingRateCheckIntervalMinutes: 10,
			RiskMonitoringIntervalMinutes:   15,
			LogLevel:                        "INFO",
			DataDir:                         "./data",
			LogDir:                          "./logs",
		},
		Redis: RedisConfig{
			Host:      "localhost",
			Port:      6379,
			Password:  "",
			DB:        0,
			KeyPrefix: "funding_bot:",
		},
		Postgres: PostgresConfig{
			Host:           "localhost",
			Port:           5432,
			Database:       "funding_arbitrage",
			User:           "postgres",
			MaxConnections: 10,
			SSLMode:        "require",
		},
		Notification: NotificationConfig{
			Telegram: TelegramConfig{
				Enabled: true,
			},
		},
	}
}

// SaveConfigToFile 将配置保存到文件
func SaveConfigToFile(config *Config, filePath string) error {
	v := viper.New()
	v.SetConfigFile(filePath)

	// 将配置转换为map
	// 注意：这里不包含敏感信息
	configMap := map[string]interface{}{
		"exchanges": map[string]interface{}{
			"binance": map[string]interface{}{
				"enabled": config.Exchanges.Binance.Enabled,
			},
			"okx": map[string]interface{}{
				"enabled": config.Exchanges.OKX.Enabled,
			},
			"bitget": map[string]interface{}{
				"enabled": config.Exchanges.Bitget.Enabled,
			},
		},
		"trading": map[string]interface{}{
			"min_yearly_funding_rate":   config.Trading.MinYearlyFundingRate,
			"max_leverage":              config.Trading.MaxLeverage,
			"max_position_size_percent": config.Trading.MaxPositionSizePercent,
			"account_usage_limit":       config.Trading.AccountUsageLimit,
			"min_profit_threshold":      config.Trading.MinProfitThreshold,
			"allowed_symbols":           config.Trading.AllowedSymbols,
		},
		"risk_management": map[string]interface{}{
			"liquidation_warning_threshold":   config.RiskManagement.LiquidationWarningThreshold,
			"liquidation_emergency_threshold": config.RiskManagement.LiquidationEmergencyThreshold,
			"hedge_rebalance_threshold":       config.RiskManagement.HedgeRebalanceThreshold,
			"max_position_time_days":          config.RiskManagement.MaxPositionTimeDays,
			"volatility_risk_weight":          config.RiskManagement.VolatilityRiskWeight,
			"liquidation_risk_weight":         config.RiskManagement.LiquidationRiskWeight,
			"liquidity_risk_weight":           config.RiskManagement.LiquidityRiskWeight,
			"exchange_risk_weight":            config.RiskManagement.ExchangeRiskWeight,
			"max_hedge_ratio_adjustment":      config.RiskManagement.MaxHedgeRatioAdjustment,
			"anomaly_z_score_threshold":       config.RiskManagement.AnomalyZScoreThreshold,
		},
		"system": map[string]interface{}{
			"funding_rate_check_interval_minutes": config.System.FundingRateCheckIntervalMinutes,
			"risk_monitoring_interval_minutes":    config.System.RiskMonitoringIntervalMinutes,
			"log_level":                           config.System.LogLevel,
			"data_dir":                            config.System.DataDir,
			"log_dir":                             config.System.LogDir,
		},
		"redis": map[string]interface{}{
			"host":       config.Redis.Host,
			"port":       config.Redis.Port,
			"password":   config.Redis.Password,
			"db":         config.Redis.DB,
			"key_prefix": config.Redis.KeyPrefix,
		},
		"postgres": map[string]interface{}{
			"host":            config.Postgres.Host,
			"port":            config.Postgres.Port,
			"database":        config.Postgres.Database,
			"user":            config.Postgres.User,
			"max_connections": config.Postgres.MaxConnections,
			"ssl_mode":        config.Postgres.SSLMode,
		},
		"notification": map[string]interface{}{
			"telegram": map[string]interface{}{
				"enabled": config.Notification.Telegram.Enabled,
			},
		},
	}

	// 将配置设置到viper
	for k, v := range configMap {
		viper.Set(k, v)
	}

	// 写入文件
	return v.WriteConfigAs(filePath)
}

package exchange

import (
	"github.com/life2you_mini/calcs/internal/config"
	"go.uber.org/zap"
)

// CreateExchangeFactory 创建交易所工厂并初始化所有交易所
func CreateExchangeFactory(
	logger *zap.Logger,
	proxy *config.HttpProxyConfig,
	binanceConfig *BinanceConfig,
	okxConfig *OKXConfig,
	bitgetConfig *BitgetConfig,
) *ExchangeFactory {
	factory := NewExchangeFactory()

	// 初始化Binance
	if binanceConfig != nil && binanceConfig.Enabled {
		binanceClient := NewBinanceClient(binanceConfig.APIKey, binanceConfig.APISecret, logger, proxy)
		factory.Register("Binance", binanceClient)
		logger.Info("Binance交易所已注册")
	}

	// 初始化OKX
	if okxConfig != nil && okxConfig.Enabled {
		okxClient := NewOKXClient(okxConfig.APIKey, okxConfig.APISecret, okxConfig.Passphrase, logger, proxy)
		factory.Register("OKX", okxClient)
		logger.Info("OKX交易所已注册")
	}

	// 初始化Bitget
	if bitgetConfig != nil && bitgetConfig.Enabled {
		bitgetClient := NewBitgetClient(bitgetConfig.APIKey, bitgetConfig.APISecret, bitgetConfig.Passphrase, logger, proxy)
		factory.Register("Bitget", bitgetClient)
		logger.Info("Bitget交易所已注册")
	}

	return factory
}

// BinanceConfig Binance配置
type BinanceConfig struct {
	Enabled   bool
	APIKey    string
	APISecret string
}

// OKXConfig OKX配置
type OKXConfig struct {
	Enabled    bool
	APIKey     string
	APISecret  string
	Passphrase string
}

// BitgetConfig Bitget配置
type BitgetConfig struct {
	Enabled    bool
	APIKey     string
	APISecret  string
	Passphrase string
}

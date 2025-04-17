package exchange

import (
	"context"
	"time"
)

// FundingRateData 资金费率数据结构
type FundingRateData struct {
	Exchange        string    `json:"exchange"`
	Symbol          string    `json:"symbol"`
	FundingRate     float64   `json:"funding_rate"`
	YearlyRate      float64   `json:"yearly_rate"`
	NextFundingTime time.Time `json:"next_funding_time"`
	Timestamp       time.Time `json:"timestamp"`
}

// Exchange 交易所接口
type Exchange interface {
	// 基础信息
	GetExchangeName() string

	// 资金费率相关
	GetFundingRate(ctx context.Context, symbol string) (*FundingRateData, error)
	GetAllFundingRates(ctx context.Context) ([]*FundingRateData, error)

	// 价格和深度相关
	GetPrice(ctx context.Context, symbol string) (float64, error)
	GetMarketDepth(ctx context.Context, symbol string) (map[string]interface{}, error)

	// 交易相关
	SetLeverage(ctx context.Context, symbol string, leverage int) error
	CreateContractOrder(ctx context.Context, symbol string, side string, positionSide string, orderType string, quantity float64, price float64) (string, error)
	CreateSpotOrder(ctx context.Context, symbol string, side string, orderType string, quantity float64, price float64) (string, error)
}

// CalculateYearlyRate 计算年化收益率
// 资金费率通常每8小时结算一次，一年约有365*3=1095次
func CalculateYearlyRate(fundingRate float64) float64 {
	return fundingRate * 1095 * 100 // 转为百分比形式
}

// ExchangeFactory 交易所工厂，用于创建支持的交易所实例
type ExchangeFactory struct {
	exchanges map[string]Exchange
}

// NewExchangeFactory 创建交易所工厂
func NewExchangeFactory() *ExchangeFactory {
	return &ExchangeFactory{
		exchanges: make(map[string]Exchange),
	}
}

// Register 注册交易所实例
func (f *ExchangeFactory) Register(name string, exchange Exchange) {
	f.exchanges[name] = exchange
}

// Get 获取交易所实例
func (f *ExchangeFactory) Get(name string) (Exchange, bool) {
	exchange, exists := f.exchanges[name]
	return exchange, exists
}

// GetAll 获取所有交易所实例
func (f *ExchangeFactory) GetAll() []Exchange {
	var result []Exchange
	for _, exchange := range f.exchanges {
		result = append(result, exchange)
	}
	return result
}

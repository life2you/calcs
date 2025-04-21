package services

import (
	"context"

	"github.com/life2you_mini/calcs/internal/exchange"
	"github.com/life2you_mini/calcs/internal/monitor"
)

// ExchangeAdapter 适配 exchange.Exchange 接口到 monitor.ExchangeAPI 接口
type ExchangeAdapter struct {
	exchange exchange.Exchange
}

// NewExchangeAdapter 创建一个新的交易所适配器
func NewExchangeAdapter(ex exchange.Exchange) *ExchangeAdapter {
	return &ExchangeAdapter{
		exchange: ex,
	}
}

// GetExchangeName 获取交易所名称
func (a *ExchangeAdapter) GetExchangeName() string {
	return a.exchange.GetExchangeName()
}

// GetFundingRate 获取指定交易对的资金费率
func (a *ExchangeAdapter) GetFundingRate(ctx context.Context, symbol string) (*monitor.FundingRateData, error) {
	// 调用原始方法
	originalData, err := a.exchange.GetFundingRate(ctx, symbol)
	if err != nil {
		return nil, err
	}

	// 转换为 monitor.FundingRateData
	return &monitor.FundingRateData{
		Exchange:    originalData.Exchange,
		Symbol:      originalData.Symbol,
		FundingRate: originalData.FundingRate,
		YearlyRate:  originalData.YearlyRate,
		Timestamp:   originalData.Timestamp,
	}, nil
}

// GetAllFundingRates 获取所有交易对的资金费率
func (a *ExchangeAdapter) GetAllFundingRates(ctx context.Context) ([]*monitor.FundingRateData, error) {
	// 调用原始方法
	originalDataList, err := a.exchange.GetAllFundingRates(ctx)
	if err != nil {
		return nil, err
	}

	// 转换为 monitor.FundingRateData 列表
	result := make([]*monitor.FundingRateData, len(originalDataList))
	for i, originalData := range originalDataList {
		result[i] = &monitor.FundingRateData{
			Exchange:    originalData.Exchange,
			Symbol:      originalData.Symbol,
			FundingRate: originalData.FundingRate,
			YearlyRate:  originalData.YearlyRate,
			Timestamp:   originalData.Timestamp,
		}
	}

	return result, nil
}

// GetMarketDepth 获取市场深度
func (a *ExchangeAdapter) GetMarketDepth(ctx context.Context, symbol string, limit int) (map[string]interface{}, error) {
	// 调用原始方法
	return a.exchange.GetMarketDepth(ctx, symbol)
}

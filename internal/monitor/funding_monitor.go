package monitor

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/life2you_mini/fundingarb/internal/exchange"
	"github.com/life2you_mini/fundingarb/internal/redis"
)

// ExchangeAPI 交易所API接口
type ExchangeAPI interface {
	GetExchangeName() string
	GetAllFundingRates(ctx context.Context) ([]*exchange.FundingRateData, error)
}

// TradingOpportunity 交易机会结构
type TradingOpportunity struct {
	Exchange    string    `json:"exchange"`
	Symbol      string    `json:"symbol"`
	FundingRate float64   `json:"funding_rate"`
	YearlyRate  float64   `json:"yearly_rate"`
	Direction   string    `json:"direction"` // "LONG" 或 "SHORT"
	Timestamp   time.Time `json:"timestamp"`
}

// FundingRateMonitor 资金费率监控器
type FundingRateMonitor struct {
	exchanges      []ExchangeAPI
	logger         *zap.Logger
	minYearlyRate  float64
	allowedSymbols []string
	checkInterval  time.Duration
	redisClient    *redis.StorageClient
}

// NewFundingRateMonitor 创建新的资金费率监控器
func NewFundingRateMonitor(
	exchanges []ExchangeAPI,
	logger *zap.Logger,
	minYearlyRate float64,
	allowedSymbols []string,
	redisClient *redis.StorageClient,
) *FundingRateMonitor {
	return &FundingRateMonitor{
		exchanges:      exchanges,
		logger:         logger,
		minYearlyRate:  minYearlyRate,
		allowedSymbols: allowedSymbols,
		checkInterval:  10 * time.Minute, // 默认10分钟检查一次
		redisClient:    redisClient,
	}
}

// SetCheckInterval 设置检查间隔
func (m *FundingRateMonitor) SetCheckInterval(interval time.Duration) {
	m.checkInterval = interval
}

// Start 启动监控
func (m *FundingRateMonitor) Start(ctx context.Context) error {
	m.logger.Info("启动资金费率监控器",
		zap.Float64("最低年化率阈值", m.minYearlyRate),
		zap.Duration("检查间隔", m.checkInterval))

	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	// 立即执行一次检查
	if err := m.checkAllExchanges(ctx); err != nil {
		m.logger.Error("首次检查资金费率失败", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := m.checkAllExchanges(ctx); err != nil {
				m.logger.Error("检查资金费率失败", zap.Error(err))
			}
		}
	}
}

// checkAllExchanges 检查所有交易所的资金费率
func (m *FundingRateMonitor) checkAllExchanges(ctx context.Context) error {
	for _, ex := range m.exchanges {
		exchangeName := ex.GetExchangeName()
		m.logger.Debug("开始检查交易所资金费率", zap.String("exchange", exchangeName))

		// 获取该交易所所有交易对的资金费率
		fundingRates, err := ex.GetAllFundingRates(ctx)
		if err != nil {
			m.logger.Error("获取资金费率失败",
				zap.String("exchange", exchangeName),
				zap.Error(err))
			continue
		}

		m.logger.Debug("获取资金费率成功",
			zap.String("exchange", exchangeName),
			zap.Int("数量", len(fundingRates)))

		// 处理并筛选套利机会
		if err := m.processFundingRates(ctx, fundingRates); err != nil {
			m.logger.Error("处理资金费率失败",
				zap.String("exchange", exchangeName),
				zap.Error(err))
		}
	}

	return nil
}

// processFundingRates 处理资金费率数据并找出套利机会
func (m *FundingRateMonitor) processFundingRates(
	ctx context.Context,
	fundingRates []*exchange.FundingRateData,
) error {
	now := time.Now()

	// 遍历所有资金费率
	for _, rate := range fundingRates {
		// 如果设置了允许的交易对列表，则只处理指定的交易对
		if len(m.allowedSymbols) > 0 {
			allowed := false
			for _, s := range m.allowedSymbols {
				if s == rate.Symbol {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}

		// 日志记录资金费率和年化率
		m.logger.Debug("资金费率信息",
			zap.String("exchange", rate.Exchange),
			zap.String("symbol", rate.Symbol),
			zap.Float64("funding_rate", rate.FundingRate),
			zap.Float64("yearly_rate", rate.YearlyRate))

		// 保存资金费率历史数据
		if err := m.saveHistoricalRate(ctx, rate.Exchange, rate.Symbol, rate.FundingRate, rate.YearlyRate, now); err != nil {
			m.logger.Warn("保存历史资金费率失败",
				zap.String("exchange", rate.Exchange),
				zap.String("symbol", rate.Symbol),
				zap.Error(err))
		}

		// 检查年化率是否达到阈值
		if abs(rate.YearlyRate) >= m.minYearlyRate {
			// 创建交易机会对象
			opportunity := TradingOpportunity{
				Exchange:    rate.Exchange,
				Symbol:      rate.Symbol,
				FundingRate: rate.FundingRate,
				YearlyRate:  rate.YearlyRate,
				Direction:   m.getDirection(rate.FundingRate),
				Timestamp:   now,
			}

			// 记录找到的套利机会
			m.logger.Info("发现套利机会",
				zap.String("exchange", opportunity.Exchange),
				zap.String("symbol", opportunity.Symbol),
				zap.Float64("funding_rate", opportunity.FundingRate),
				zap.Float64("yearly_rate", opportunity.YearlyRate),
				zap.String("direction", opportunity.Direction))

			// 将机会添加到交易队列
			if err := m.addToTradeQueue(ctx, opportunity); err != nil {
				m.logger.Error("添加套利机会到队列失败",
					zap.String("exchange", opportunity.Exchange),
					zap.String("symbol", opportunity.Symbol),
					zap.Error(err))
				continue
			}
		}
	}

	return nil
}

// saveHistoricalRate 保存历史资金费率数据
func (m *FundingRateMonitor) saveHistoricalRate(
	ctx context.Context,
	exchange string,
	symbol string,
	rate float64,
	yearlyRate float64,
	timestamp time.Time,
) error {
	// 构建键名
	key := fmt.Sprintf("funding_rates:%s:%s", exchange, symbol)

	// 将资金费率数据存储到Redis时间序列中
	return m.redisClient.SaveFundingRate(ctx, key, rate, yearlyRate, timestamp)
}

// addToTradeQueue 将交易机会添加到队列
func (m *FundingRateMonitor) addToTradeQueue(ctx context.Context, opportunity TradingOpportunity) error {
	return m.redisClient.AddToTradeQueue(ctx, opportunity)
}

// getDirection 根据资金费率决定交易方向
func (m *FundingRateMonitor) getDirection(rate float64) string {
	if rate > 0 {
		// 正资金费率，做空合约能收取资金费
		return "SHORT"
	} else {
		// 负资金费率，做多合约能收取资金费
		return "LONG"
	}
}

// abs 取绝对值
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

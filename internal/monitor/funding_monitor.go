package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	redisLib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/life2you_mini/calcs/internal/exchange"
)

// 常量定义
const (
	MinYearlyRateThreshold     = 0.10 // 10%
	FundingRateCheckInterval   = 5 * time.Minute
	HistoricalRateStorageDays  = 30
	DefaultRateHistoryCapacity = 24 * 7 // 一周的数据点 (假设8小时一次)
)

// ExchangeAPI 交易所API接口
type ExchangeAPI interface {
	GetExchangeName() string
	GetFundingRate(ctx context.Context, symbol string) (*FundingRateData, error)
	GetAllFundingRates(ctx context.Context) ([]*FundingRateData, error)
	GetMarketDepth(ctx context.Context, symbol string, limit int) (map[string]interface{}, error)
}

// RedisClientInterface Redis客户端接口
type RedisClientInterface interface {
	SaveFundingRate(ctx context.Context, exchange, symbol string, rate, yearlyRate float64, timestamp time.Time) error
	GetFundingRateHistory(ctx context.Context, exchange, symbol string, startTime, endTime time.Time) ([]FundingRateData, error)
	GetPositionForRiskMonitoring(ctx context.Context) ([]*exchange.Position, error)
	GetPositionForRiskMonitoringStr(ctx context.Context) ([]string, error)
	SetLock(ctx context.Context, key string, expiration time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, key string) error
	PushTask(ctx context.Context, queue string, task interface{}) error
	PushTaskWithPriority(ctx context.Context, queue string, task interface{}, priority int) error
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	GetJSON(ctx context.Context, key string, dest interface{}) error
	Client() *redisLib.Client
}

// FundingRateData 资金费率数据
type FundingRateData struct {
	Exchange    string    `json:"exchange"`
	Symbol      string    `json:"symbol"`
	FundingRate float64   `json:"funding_rate"`
	YearlyRate  float64   `json:"yearly_rate"`
	Timestamp   time.Time `json:"timestamp"`
}

// FundingOpportunity 资金费率套利机会
type FundingOpportunity struct {
	Exchange      string    `json:"exchange"`
	Symbol        string    `json:"symbol"`
	FundingRate   float64   `json:"funding_rate"`
	YearlyRate    float64   `json:"yearly_rate"`
	FirstObserved time.Time `json:"first_observed"`
	LastUpdated   time.Time `json:"last_updated"`
	Consecutive   int       `json:"consecutive"` // 连续观察到的次数
}

// FundingRateMonitor 资金费率监控组件
type FundingRateMonitor struct {
	exchanges      []ExchangeAPI
	logger         *zap.Logger
	minYearlyRate  float64
	allowedSymbols []string
	rateHistoryMap map[string][]FundingRateData   // key: exchange:symbol
	opportunityMap map[string]*FundingOpportunity // key: exchange:symbol
	redisClient    RedisClientInterface
	mu             sync.RWMutex
	checkInterval  time.Duration // 检查间隔
}

// NewFundingRateMonitor 创建资金费率监控组件
func NewFundingRateMonitor(
	exchanges []ExchangeAPI,
	logger *zap.Logger,
	minYearlyRate float64,
	allowedSymbols []string,
	redisClient RedisClientInterface,
) *FundingRateMonitor {
	if minYearlyRate <= 0 {
		minYearlyRate = MinYearlyRateThreshold
	}

	return &FundingRateMonitor{
		exchanges:      exchanges,
		logger:         logger,
		minYearlyRate:  minYearlyRate,
		allowedSymbols: allowedSymbols,
		rateHistoryMap: make(map[string][]FundingRateData),
		opportunityMap: make(map[string]*FundingOpportunity),
		redisClient:    redisClient,
		mu:             sync.RWMutex{},
		checkInterval:  FundingRateCheckInterval, // 默认检查间隔
	}
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

// SetCheckInterval 设置检查间隔
func (m *FundingRateMonitor) SetCheckInterval(interval time.Duration) {
	if interval <= 0 {
		interval = FundingRateCheckInterval // 使用默认值
	}
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
		for _, rate := range fundingRates {
			if err := m.processFundingRate(ctx, rate); err != nil {
				m.logger.Error("处理资金费率失败",
					zap.String("exchange", exchangeName),
					zap.String("symbol", rate.Symbol),
					zap.Error(err))
			}
		}
	}

	return nil
}

// processFundingRate 处理单个资金费率数据
func (m *FundingRateMonitor) processFundingRate(
	ctx context.Context,
	rate *FundingRateData,
) error {
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
			return nil
		}
	}

	// 日志记录资金费率和年化率
	m.logger.Debug("资金费率信息",
		zap.String("exchange", rate.Exchange),
		zap.String("symbol", rate.Symbol),
		zap.Float64("funding_rate", rate.FundingRate),
		zap.Float64("yearly_rate", rate.YearlyRate))

	// 保存资金费率历史数据
	now := time.Now()
	if err := m.saveHistoricalRate(ctx, rate); err != nil {
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
		}
	}

	return nil
}

// saveHistoricalRate 保存历史资金费率数据
func (m *FundingRateMonitor) saveHistoricalRate(
	ctx context.Context,
	rate *FundingRateData,
) error {
	// 保存到Redis
	err := m.redisClient.SaveFundingRate(
		ctx,
		rate.Exchange,
		rate.Symbol,
		rate.FundingRate,
		rate.YearlyRate,
		rate.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("保存资金费率到Redis失败: %w", err)
	}

	// 更新内存中的历史数据
	key := fmt.Sprintf("%s:%s", rate.Exchange, rate.Symbol)

	m.mu.Lock()
	defer m.mu.Unlock()

	history, ok := m.rateHistoryMap[key]
	if !ok {
		history = make([]FundingRateData, 0, DefaultRateHistoryCapacity)
	}

	// 添加最新数据
	history = append(history, *rate)

	// 保持历史容量合理
	if len(history) > DefaultRateHistoryCapacity {
		history = history[1:]
	}

	m.rateHistoryMap[key] = history

	return nil
}

// addToTradeQueue 将交易机会添加到队列
func (m *FundingRateMonitor) addToTradeQueue(
	ctx context.Context,
	opportunity TradingOpportunity,
) error {
	// 将机会转换为任务并添加到队列
	return m.redisClient.PushTask(ctx, "funding_opportunities", opportunity)
}

// getDirection 根据资金费率决定做多还是做空
func (m *FundingRateMonitor) getDirection(fundingRate float64) string {
	if fundingRate < 0 {
		return "LONG" // 资金费率为负，做多可以收取资金费
	}
	return "SHORT" // 资金费率为正，做空可以收取资金费
}

// abs 返回绝对值
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

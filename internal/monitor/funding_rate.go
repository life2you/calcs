package monitor

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// 常量定义
const (
	FundingRateCheckInterval = 10 * time.Minute // 常规检查间隔
	EmergencyCheckInterval   = 3 * time.Minute  // 紧急检查间隔
	MinYearlyRateThreshold   = 50.0             // 最小年化率阈值(%)
	FundingIntervalPerDay    = 3                // 每天结算次数
	DaysPerYear              = 365              // 一年天数
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

// FundingOpportunity 资金费率套利机会
type FundingOpportunity struct {
	Exchange          string    `json:"exchange"`
	Symbol            string    `json:"symbol"`
	FundingRate       float64   `json:"funding_rate"`
	YearlyRate        float64   `json:"yearly_rate"`
	ContractDirection string    `json:"contract_direction"`
	SpotDirection     string    `json:"spot_direction"`
	SuggestedAmount   float64   `json:"suggested_amount"`
	Leverage          int       `json:"leverage"`
	Score             float64   `json:"score"`
	NextFundingTime   time.Time `json:"next_funding_time"`
	Timestamp         time.Time `json:"timestamp"`
}

// ExchangeAPI 交易所API接口
type ExchangeAPI interface {
	GetFundingRate(ctx context.Context, symbol string) (*FundingRateData, error)
	GetAllFundingRates(ctx context.Context) ([]*FundingRateData, error)
	GetExchangeName() string
	GetMarketDepth(ctx context.Context, symbol string) (map[string]interface{}, error)
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
}

// RedisClientInterface Redis客户端接口
type RedisClientInterface interface {
	PushTask(ctx context.Context, queueName string, task interface{}) error
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	GetJSON(ctx context.Context, key string, v interface{}) error
	PushTaskWithPriority(ctx context.Context, queueName string, task interface{}, priority int) error
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
	}
}

// Start 启动监控
func (m *FundingRateMonitor) Start(ctx context.Context) error {
	m.logger.Info("启动资金费率监控服务")

	// 立即执行一次检查
	if err := m.checkAllExchanges(ctx); err != nil {
		m.logger.Error("初始检查失败", zap.Error(err))
	}

	// 定时检查
	ticker := time.NewTicker(FundingRateCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("资金费率监控服务停止")
			return nil
		case <-ticker.C:
			if err := m.checkAllExchanges(ctx); err != nil {
				m.logger.Error("资金费率检查失败", zap.Error(err))
			}

			// 根据市场波动情况调整检查频率
			if m.shouldUseEmergencyInterval() {
				ticker.Reset(EmergencyCheckInterval)
				m.logger.Info("进入紧急检查模式", zap.Duration("interval", EmergencyCheckInterval))
			} else {
				ticker.Reset(FundingRateCheckInterval)
			}
		}
	}
}

// 检查所有交易所
func (m *FundingRateMonitor) checkAllExchanges(ctx context.Context) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	for _, exchange := range m.exchanges {
		wg.Add(1)
		go func(e ExchangeAPI) {
			defer wg.Done()

			data, err := e.GetAllFundingRates(ctx)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("获取%s资金费率失败: %w", e.GetExchangeName(), err))
				mu.Unlock()
				return
			}

			m.processFundingRates(ctx, data)
		}(exchange)
	}

	wg.Wait()

	if len(errs) > 0 {
		// 只返回第一个错误
		return errs[0]
	}

	return nil
}

// 处理资金费率数据
func (m *FundingRateMonitor) processFundingRates(ctx context.Context, data []*FundingRateData) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rate := range data {
		// 检查是否在允许的交易对列表中
		if !m.isAllowedSymbol(rate.Symbol) {
			continue
		}

		// 计算年化收益率
		yearlyRate := m.calculateYearlyRate(rate.FundingRate)
		rate.YearlyRate = yearlyRate

		// 更新历史数据
		key := fmt.Sprintf("%s:%s", rate.Exchange, rate.Symbol)
		m.rateHistoryMap[key] = append(m.rateHistoryMap[key], *rate)

		// 保留最近72个数据点（24小时，假设每10分钟一个数据点）
		if len(m.rateHistoryMap[key]) > 72 {
			m.rateHistoryMap[key] = m.rateHistoryMap[key][len(m.rateHistoryMap[key])-72:]
		}

		// 检查是否超过阈值
		if math.Abs(yearlyRate) > m.minYearlyRate {
			opportunity := &FundingOpportunity{
				Exchange:        rate.Exchange,
				Symbol:          rate.Symbol,
				FundingRate:     rate.FundingRate,
				YearlyRate:      yearlyRate,
				Timestamp:       rate.Timestamp,
				NextFundingTime: rate.NextFundingTime,
			}

			// 设置交易方向
			if rate.FundingRate > 0 {
				opportunity.ContractDirection = "SHORT" // 空合约
				opportunity.SpotDirection = "BUY"       // 买现货
			} else {
				opportunity.ContractDirection = "LONG" // 多合约
				opportunity.SpotDirection = "SELL"     // 卖现货
			}

			// 设置建议杠杆（此处简化，实际应根据波动率动态计算）
			opportunity.Leverage = 3

			// 计算评分
			opportunity.Score = m.calculateOpportunityScore(opportunity)

			// 更新机会映射
			m.opportunityMap[key] = opportunity

			// 推送到交易任务队列
			m.pushToTradeQueue(ctx, opportunity)

			m.logger.Info("发现资金费率套利机会",
				zap.String("exchange", rate.Exchange),
				zap.String("symbol", rate.Symbol),
				zap.Float64("funding_rate", rate.FundingRate),
				zap.Float64("yearly_rate", yearlyRate),
				zap.String("contract_direction", opportunity.ContractDirection),
				zap.Float64("score", opportunity.Score),
			)
		} else {
			// 如果之前有机会，现在没有了，从映射中移除
			if _, exists := m.opportunityMap[key]; exists {
				delete(m.opportunityMap, key)
				m.logger.Info("资金费率机会消失",
					zap.String("exchange", rate.Exchange),
					zap.String("symbol", rate.Symbol),
					zap.Float64("funding_rate", rate.FundingRate),
					zap.Float64("yearly_rate", yearlyRate),
				)
			}
		}
	}
}

// 计算年化收益率
func (m *FundingRateMonitor) calculateYearlyRate(fundingRate float64) float64 {
	// 每年结算次数 = 每天结算次数 * 天数
	settlementPerYear := FundingIntervalPerDay * DaysPerYear

	// 年化收益率 = 资金费率 * 每年结算次数 * 100%
	yearlyRate := fundingRate * float64(settlementPerYear) * 100.0

	// 保留2位小数
	return math.Round(yearlyRate*100) / 100
}

// 判断交易对是否在允许列表中
func (m *FundingRateMonitor) isAllowedSymbol(symbol string) bool {
	// 如果允许列表为空，则允许所有交易对
	if len(m.allowedSymbols) == 0 {
		return true
	}

	for _, s := range m.allowedSymbols {
		if s == symbol {
			return true
		}
	}

	return false
}

// 推送到交易任务队列
func (m *FundingRateMonitor) pushToTradeQueue(ctx context.Context, opportunity *FundingOpportunity) {
	// 设置优先级
	priority := int(-math.Abs(opportunity.YearlyRate))

	err := m.redisClient.PushTaskWithPriority(ctx, "trade_opportunities", opportunity, priority)
	if err != nil {
		m.logger.Error("推送套利机会到队列失败",
			zap.Error(err),
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
		)
	}
}

// 计算套利机会评分
func (m *FundingRateMonitor) calculateOpportunityScore(opportunity *FundingOpportunity) float64 {
	// 基础分数是年化收益率的绝对值
	baseScore := math.Abs(opportunity.YearlyRate)

	// 计算持续性分数 (根据历史数据评估)
	persistenceScore := m.calculatePersistenceScore(opportunity)

	// 计算流动性分数 (此处简化，实际应查询市场深度)
	liquidityScore := 80.0

	// 综合评分，权重可调整
	finalScore := baseScore*0.6 + persistenceScore*0.2 + liquidityScore*0.2

	return math.Round(finalScore*100) / 100
}

// 计算持续性评分
func (m *FundingRateMonitor) calculatePersistenceScore(opportunity *FundingOpportunity) float64 {
	key := fmt.Sprintf("%s:%s", opportunity.Exchange, opportunity.Symbol)

	// 获取历史数据
	history, exists := m.rateHistoryMap[key]
	if !exists || len(history) < 6 {
		// 没有足够的历史数据，给中等分数
		return 50.0
	}

	// 计算过去的高资金费率持续时间
	var consecutiveHighRateCount int
	threshold := m.minYearlyRate

	// 从最近的开始往前查找连续高于阈值的记录
	for i := len(history) - 1; i >= 0; i-- {
		if math.Abs(history[i].YearlyRate) > threshold {
			consecutiveHighRateCount++
		} else {
			break
		}
	}

	// 持续时间越长，分数越高
	// 12个点=2小时，最高分100
	score := float64(consecutiveHighRateCount) / 12.0 * 100
	if score > 100 {
		score = 100
	}

	return score
}

// 判断是否应该使用紧急检查间隔
func (m *FundingRateMonitor) shouldUseEmergencyInterval() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 如果有任何机会的年化率超过100%，进入紧急模式
	for _, opportunity := range m.opportunityMap {
		if math.Abs(opportunity.YearlyRate) > 100.0 {
			return true
		}
	}

	// 或者当机会数量超过一定阈值时
	if len(m.opportunityMap) >= 5 {
		return true
	}

	return false
}

// GetCurrentOpportunities 获取当前的套利机会
func (m *FundingRateMonitor) GetCurrentOpportunities() []*FundingOpportunity {
	m.mu.RLock()
	defer m.mu.RUnlock()

	opportunities := make([]*FundingOpportunity, 0, len(m.opportunityMap))
	for _, opp := range m.opportunityMap {
		opportunities = append(opportunities, opp)
	}

	return opportunities
}

// GetFundingRateHistory 获取资金费率历史
func (m *FundingRateMonitor) GetFundingRateHistory(exchange, symbol string) []FundingRateData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", exchange, symbol)
	history, exists := m.rateHistoryMap[key]
	if !exists {
		return []FundingRateData{}
	}

	return history
}

// CalculateSuggestedAmount 计算建议交易数量
func (m *FundingRateMonitor) CalculateSuggestedAmount(
	ctx context.Context,
	opportunity *FundingOpportunity,
	availableFunds float64,
	maxPositionPercent float64,
	currentPrice float64,
) (float64, error) {
	// 计算可用于该机会的最大资金
	maxFunds := availableFunds * maxPositionPercent

	// 根据杠杆计算可以开的最大仓位
	maxPositionSize := maxFunds * float64(opportunity.Leverage) / currentPrice

	// 根据流动性和风险调整实际开仓数量
	adjustedSize := maxPositionSize * 0.8 // 保守起见，只用80%

	// 四舍五入到合适的精度
	precision := m.getSymbolPrecision(opportunity.Symbol)
	scale := math.Pow(10, float64(precision))
	adjustedSize = math.Floor(adjustedSize*scale) / scale

	return adjustedSize, nil
}

// 获取交易对精度（小数位数）
func (m *FundingRateMonitor) getSymbolPrecision(symbol string) int {
	// 不同交易对有不同的精度要求，这里简化处理
	// 实际应用中应从交易所获取精度信息

	// 默认精度
	precision := 4

	// 一些常见交易对的精度
	switch symbol {
	case "BTC/USDT":
		precision = 3
	case "ETH/USDT":
		precision = 3
	case "SOL/USDT":
		precision = 2
	case "XRP/USDT":
		precision = 1
	}

	return precision
}

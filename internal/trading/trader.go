package trading

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/life2you_mini/fundingarb/internal/config"
	"github.com/life2you_mini/fundingarb/internal/exchange"
	"github.com/life2you_mini/fundingarb/internal/storage"
)

const (
	// 队列名称常量
	QueueTradeOpportunities = "trade_opportunities"
	QueueRiskMonitoring     = "risk_monitoring"

	// 任务处理超时
	taskProcessTimeout = 30 * time.Second

	// 交易方向
	DirectionLong  = "LONG"
	DirectionShort = "SHORT"

	// 交易动作
	ActionOpen   = "OPEN"
	ActionClose  = "CLOSE"
	ActionAdjust = "ADJUST"

	// 持仓状态
	StatusOpen      = "OPEN"
	StatusClosed    = "CLOSED"
	StatusAdjusting = "ADJUSTING"
)

// Trader 交易执行器
type Trader struct {
	ctx             context.Context
	cancel          context.CancelFunc
	logger          *zap.Logger
	config          *config.Config
	redisClient     *storage.RedisClient
	exchangeFactory *exchange.ExchangeFactory
	positionManager *PositionManager
	wg              sync.WaitGroup
	isRunning       bool
	mutex           sync.Mutex
}

// NewTrader 创建新的交易执行器
func NewTrader(
	parentCtx context.Context,
	cfg *config.Config,
	logger *zap.Logger,
	redisClient *storage.RedisClient,
	exchangeFactory *exchange.ExchangeFactory,
) *Trader {
	ctx, cancel := context.WithCancel(parentCtx)

	// 创建持仓管理器
	positionManager := NewPositionManager(
		logger.With(zap.String("component", "position_manager")),
		redisClient.Client(),
		"funding_bot:",
	)

	return &Trader{
		ctx:             ctx,
		cancel:          cancel,
		logger:          logger.With(zap.String("component", "trader")),
		config:          cfg,
		redisClient:     redisClient,
		exchangeFactory: exchangeFactory,
		positionManager: positionManager,
	}
}

// Start 启动交易执行器
func (t *Trader) Start() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.isRunning {
		return fmt.Errorf("交易执行器已在运行")
	}

	t.logger.Info("启动交易执行器")
	t.isRunning = true

	// 启动交易机会处理协程
	t.wg.Add(1)
	go t.processTradeOpportunities()

	return nil
}

// Stop 停止交易执行器
func (t *Trader) Stop() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if !t.isRunning {
		return nil
	}

	t.logger.Info("停止交易执行器")
	t.cancel()

	// 等待所有协程结束
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()

	// 等待最多5秒钟
	select {
	case <-done:
		t.logger.Info("交易执行器已停止")
	case <-time.After(5 * time.Second):
		t.logger.Warn("交易执行器停止超时")
	}

	t.isRunning = false
	return nil
}

// processTradeOpportunities 处理交易机会队列
func (t *Trader) processTradeOpportunities() {
	defer t.wg.Done()

	t.logger.Info("开始处理交易机会队列")

	for {
		// 检查上下文是否已取消
		select {
		case <-t.ctx.Done():
			t.logger.Info("结束交易机会处理")
			return
		default:
			// 继续处理
		}

		// 从队列获取交易机会
		taskBytes, err := t.redisClient.PopFromTradeQueue(t.ctx, 5*time.Second)
		if err != nil {
			if err != redis.Nil {
				t.logger.Error("从交易队列获取任务失败", zap.Error(err))
			}
			// 短暂休眠以避免CPU过度使用
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// 如果队列为空，继续下一轮
		if taskBytes == nil {
			continue
		}

		// 解析交易机会
		var opportunity TradeOpportunity
		if err := json.Unmarshal(taskBytes, &opportunity); err != nil {
			t.logger.Error("解析交易机会失败", zap.Error(err), zap.String("data", string(taskBytes)))
			continue
		}

		// 创建处理上下文
		processCtx, cancel := context.WithTimeout(t.ctx, taskProcessTimeout)

		// 处理交易机会
		t.logger.Info("处理交易机会",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.Float64("funding_rate", opportunity.FundingRate),
			zap.Float64("yearly_rate", opportunity.YearlyRate),
			zap.String("direction", opportunity.Direction))

		if err := t.handleTradeOpportunity(processCtx, &opportunity); err != nil {
			t.logger.Error("处理交易机会失败",
				zap.String("exchange", opportunity.Exchange),
				zap.String("symbol", opportunity.Symbol),
				zap.Error(err))
		}

		cancel() // 释放上下文
	}
}

// handleTradeOpportunity 处理单个交易机会
func (t *Trader) handleTradeOpportunity(ctx context.Context, opportunity *TradeOpportunity) error {
	// 1. 决策是否执行交易
	decision, err := t.makeTradeDecision(ctx, opportunity)
	if err != nil {
		return fmt.Errorf("交易决策失败: %w", err)
	}

	// 如果决定不交易
	if decision == nil {
		t.logger.Info("决定不执行交易",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.String("reason", "不满足交易条件"))
		return nil
	}

	// 2. 执行交易
	result, err := t.executeTradeDecision(ctx, decision)
	if err != nil {
		return fmt.Errorf("执行交易失败: %w", err)
	}

	// 3. 处理交易结果
	if result.Success {
		t.logger.Info("交易执行成功",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.String("action", decision.Action))

		// 如果是开仓，将持仓推送到风险监控队列
		if decision.Action == ActionOpen && result.Position != nil {
			if err := t.addToRiskMonitoringQueue(ctx, result.Position); err != nil {
				t.logger.Error("添加持仓到风险监控队列失败",
					zap.String("position_id", result.Position.ID),
					zap.Error(err))
			}
		}
	} else {
		t.logger.Error("交易执行失败",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.String("reason", result.FailReason))
	}

	return nil
}

// makeTradeDecision 根据交易机会做出交易决策
func (t *Trader) makeTradeDecision(ctx context.Context, opportunity *TradeOpportunity) (*TradeDecision, error) {
	// 获取交易所客户端
	ex, err := t.exchangeFactory.GetExchange(opportunity.Exchange)
	if err != nil {
		return nil, fmt.Errorf("获取交易所客户端失败: %w", err)
	}

	// 验证资金费率是否仍然满足条件
	currentRate, err := ex.GetFundingRate(ctx, opportunity.Symbol)
	if err != nil {
		return nil, fmt.Errorf("获取最新资金费率失败: %w", err)
	}

	// 计算当前年化收益率
	currentYearlyRate := exchange.CalculateYearlyRate(currentRate)
	minRate := t.config.Trading.MinYearlyFundingRate

	// 验证年化收益率是否仍然高于阈值
	if math.Abs(currentYearlyRate) < minRate {
		t.logger.Info("资金费率不再满足条件",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.Float64("current_yearly_rate", currentYearlyRate),
			zap.Float64("min_yearly_rate", minRate))
		return nil, nil
	}

	// 根据资金费率方向决定交易方向
	direction := DirectionLong
	if currentRate > 0 {
		direction = DirectionShort
	}

	// 获取当前价格
	currentPrice, err := ex.GetCurrentPrice(ctx, opportunity.Symbol)
	if err != nil {
		return nil, fmt.Errorf("获取当前价格失败: %w", err)
	}

	// 计算交易规模
	contractSize, spotSize, leverage, err := t.calculateTradeSize(ctx, ex, opportunity.Symbol, currentPrice)
	if err != nil {
		return nil, fmt.Errorf("计算交易规模失败: %w", err)
	}

	// 如果计算出的交易规模为0，则不执行交易
	if contractSize <= 0 || spotSize <= 0 {
		t.logger.Info("计算的交易规模过小，取消交易",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol))
		return nil, nil
	}

	// 估算预期收益
	estimatedProfit := t.estimateProfit(currentRate, contractSize, currentPrice)

	// 创建交易决策
	decision := &TradeDecision{
		Opportunity:     *opportunity,
		Action:          ActionOpen,
		ContractSize:    contractSize,
		SpotSize:        spotSize,
		Leverage:        leverage,
		EstimatedProfit: estimatedProfit,
		EntryPrice:      currentPrice,
		Reason:          fmt.Sprintf("资金费率年化收益 %.2f%% 高于阈值 %.2f%%", currentYearlyRate, minRate),
	}

	t.logger.Info("生成交易决策",
		zap.String("exchange", opportunity.Exchange),
		zap.String("symbol", opportunity.Symbol),
		zap.String("action", decision.Action),
		zap.Float64("contract_size", decision.ContractSize),
		zap.Float64("spot_size", decision.SpotSize),
		zap.Int("leverage", decision.Leverage),
		zap.Float64("estimated_profit", decision.EstimatedProfit))

	return decision, nil
}

// calculateTradeSize 计算交易规模
func (t *Trader) calculateTradeSize(
	ctx context.Context,
	ex exchange.Exchange,
	symbol string,
	price float64,
) (contractSize, spotSize float64, leverage int, err error) {
	// 获取账户余额信息
	balance, err := ex.GetAccountBalance(ctx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("获取账户余额失败: %w", err)
	}

	// 从配置读取参数
	maxPositionSizePercent := t.config.Trading.MaxPositionSizePercent
	accountUsageLimit := t.config.Trading.AccountUsageLimit
	maxLeverage := t.config.Trading.MaxLeverage

	// 计算最大可用资金（考虑账户使用限制和单个头寸限制）
	maxUsableFunds := balance.AvailableUSDT * accountUsageLimit
	maxPositionFunds := balance.TotalUSDT * maxPositionSizePercent

	// 取较小值作为实际可用资金
	availableFunds := math.Min(maxUsableFunds, maxPositionFunds)

	// 如果可用资金不足，返回0
	if availableFunds < 10 { // 最低10美元
		t.logger.Warn("可用资金不足",
			zap.Float64("available_funds", availableFunds),
			zap.Float64("min_required", 10))
		return 0, 0, 0, nil
	}

	// 根据风险计算最优杠杆
	optimizedLeverage := t.calculateOptimalLeverage(symbol, price)
	if optimizedLeverage > maxLeverage {
		optimizedLeverage = maxLeverage
	}
	if optimizedLeverage < 1 {
		optimizedLeverage = 1
	}

	// 计算合约和现货规模
	contractValue := availableFunds * float64(optimizedLeverage)
	contractSize = contractValue / price

	// 现货规模与合约规模相同（1:1对冲）
	spotSize = contractSize

	// 调整到交易所最小单位
	contractSize = adjustToMinLot(contractSize, 0.001) // 假设最小单位为0.001
	spotSize = adjustToMinLot(spotSize, 0.001)         // 假设最小单位为0.001

	return contractSize, spotSize, optimizedLeverage, nil
}

// 计算最优杠杆倍数
func (t *Trader) calculateOptimalLeverage(symbol string, price float64) int {
	// 简单实现，根据交易对类型和价格风险调整杠杆
	// 真实实现应考虑更多因素如波动率，资金费率大小等

	// 默认杠杆
	baseLeverage := 3

	// 根据标的资产调整杠杆
	switch {
	case symbol == "BTC/USDT":
		baseLeverage = 5 // 比特币流动性高，可用更高杠杆
	case symbol == "ETH/USDT":
		baseLeverage = 4 // 以太坊次之
	default:
		baseLeverage = 3 // 其他币种保守一些
	}

	return baseLeverage
}

// 估算收益
func (t *Trader) estimateProfit(fundingRate float64, size float64, price float64) float64 {
	// 估算一天的资金费
	// 一般交易所8小时收一次资金费，一天3次
	dailyFundingProfit := fundingRate * 3 * size * price

	return dailyFundingProfit
}

// 调整到最小交易单位
func adjustToMinLot(size float64, minLot float64) float64 {
	return math.Floor(size/minLot) * minLot
}

// executeTradeDecision 执行交易决策
func (t *Trader) executeTradeDecision(ctx context.Context, decision *TradeDecision) (*TradeResult, error) {
	// TODO: 实现交易执行逻辑
	// 1. 设置杠杆
	// 2. 执行合约交易（多/空）
	// 3. 执行现货交易（买/卖）
	// 4. 验证交易结果
	// 5. 创建持仓记录

	// 临时实现，仅记录交易决策但不实际执行
	t.logger.Info("模拟执行交易决策",
		zap.String("exchange", decision.Opportunity.Exchange),
		zap.String("symbol", decision.Opportunity.Symbol),
		zap.String("action", decision.Action),
		zap.Float64("contract_size", decision.ContractSize),
		zap.Float64("spot_size", decision.SpotSize),
		zap.Int("leverage", decision.Leverage))

	// 创建模拟交易结果
	now := time.Now()
	position := &Position{
		ID:               "sim_" + fmt.Sprintf("%d", now.UnixNano()),
		Exchange:         decision.Opportunity.Exchange,
		Symbol:           decision.Opportunity.Symbol,
		Direction:        decision.Opportunity.Direction,
		ContractSize:     decision.ContractSize,
		SpotSize:         decision.SpotSize,
		EntryPrice:       decision.EntryPrice,
		Leverage:         decision.Leverage,
		OpenTime:         now,
		LastUpdateTime:   now,
		ContractOrderID:  "simulated",
		SpotOrderID:      "simulated",
		FundingCollected: 0,
		Status:           StatusOpen,
	}

	result := &TradeResult{
		Decision:      *decision,
		Position:      position,
		Success:       true,
		ExecutionTime: now,
	}

	// 在实际实现中，应当将持仓保存到数据库
	// 现在只是模拟保存
	t.logger.Info("模拟保存持仓记录", zap.String("position_id", position.ID))

	return result, nil
}

// addToRiskMonitoringQueue 将持仓添加到风险监控队列
func (t *Trader) addToRiskMonitoringQueue(ctx context.Context, position *Position) error {
	// 序列化持仓数据
	positionData, err := json.Marshal(position)
	if err != nil {
		return fmt.Errorf("序列化持仓数据失败: %w", err)
	}

	// 添加到风险监控队列
	// TODO: 在实际实现中，使用Redis队列服务
	t.logger.Info("模拟将持仓添加到风险监控队列", zap.String("position_id", position.ID))

	return nil
}

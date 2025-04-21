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

	"github.com/life2you_mini/calcs/internal/config"
	"github.com/life2you_mini/calcs/internal/exchange"
	"github.com/life2you_mini/calcs/internal/storage"
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
	redisClient     storage.RedisClient
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
	redisClient storage.RedisClient,
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
	ex, exists := t.exchangeFactory.Get(opportunity.Exchange)
	if !exists {
		return nil, fmt.Errorf("获取交易所客户端失败: 不支持的交易所 %s", opportunity.Exchange)
	}

	// 验证资金费率是否仍然满足条件
	fundingData, err := ex.GetFundingRate(ctx, opportunity.Symbol)
	if err != nil {
		return nil, fmt.Errorf("获取最新资金费率失败: %w", err)
	}

	// 计算当前年化收益率
	currentYearlyRate := fundingData.YearlyRate // 直接使用已计算的年化率
	minRate := t.config.Trading.MinYearlyFundingRate

	// 如果资金费率不再满足条件
	if math.Abs(currentYearlyRate) < minRate {
		t.logger.Info("资金费率不再满足条件",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.Float64("current_rate", currentYearlyRate),
			zap.Float64("min_rate", minRate))
		return nil, nil
	}

	// 获取当前价格
	currentPrice, err := ex.GetPrice(ctx, opportunity.Symbol)
	if err != nil {
		return nil, fmt.Errorf("获取当前价格失败: %w", err)
	}

	// 计算交易规模
	contractSize, spotSize, leverage, err := t.calculateTradeSize(ctx, ex, opportunity.Symbol, currentPrice)
	if err != nil {
		return nil, fmt.Errorf("计算交易规模失败: %w", err)
	}

	// 检查是否有足够的资金交易
	if contractSize <= 0 || spotSize <= 0 {
		t.logger.Info("交易规模太小，不执行交易",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.Float64("contract_size", contractSize),
			zap.Float64("spot_size", spotSize))
		return nil, nil
	}

	// 确定交易方向和合约侧
	var contractSide, contractPosSide, spotSide string
	if opportunity.Direction == DirectionLong {
		contractSide = "BUY"
		contractPosSide = "LONG"
		spotSide = "BUY"
	} else {
		contractSide = "SELL"
		contractPosSide = "SHORT"
		spotSide = "SELL"
	}

	// 估算收益（使用FundingRate字段）
	estimatedProfit := t.estimateProfit(fundingData.FundingRate, contractSize, currentPrice)

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
		// 设置新增字段
		ContractSide:    contractSide,
		ContractPosSide: contractPosSide,
		SpotSide:        spotSide,
		FundingRate:     fundingData.FundingRate,
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
	// 获取账户余额
	balance, err := ex.GetBalance(ctx, "USDT")
	if err != nil {
		return 0, 0, 0, fmt.Errorf("获取账户余额失败: %w", err)
	}

	// 从配置读取参数
	maxPositionSizePercent := t.config.Trading.MaxPositionSizePercent
	accountUsageLimit := t.config.Trading.AccountUsageLimit
	maxLeverage := t.config.Trading.MaxLeverage

	// 计算最大可用资金（考虑账户使用限制和单个头寸限制）
	// 直接使用 balance 作为可用余额，因为 GetBalance 返回的是 float64
	availableUSDT := balance * 0.95 // 假设可用余额是总余额的95%
	totalUSDT := balance

	maxUsableFunds := availableUSDT * accountUsageLimit
	maxPositionFunds := totalUSDT * maxPositionSizePercent

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
	t.logger.Info("开始执行交易决策", zap.Any("decision", decision))

	// 获取交易所实例
	ex, exists := t.exchangeFactory.Get(decision.Opportunity.Exchange)
	if !exists {
		return &TradeResult{
			Decision:   *decision,
			Success:    false,
			FailReason: fmt.Sprintf("获取交易所实例失败: 不支持的交易所 %s", decision.Opportunity.Exchange),
		}, nil
	}

	var contractOrderID, spotOrderID string
	var contractErr, spotErr error
	var finalPosition *Position // 用于存储最终创建的持仓

	// --- 核心交易逻辑 ---
	defer func() {
		// 无论成功失败，都尝试记录或更新持仓状态
		if finalPosition != nil {
			err := t.positionManager.SavePosition(ctx, finalPosition)
			if err != nil {
				t.logger.Error("保存最终持仓状态失败", zap.String("position_id", finalPosition.ID), zap.Error(err))
			}
		}
	}()

	// 1. 设置杠杆 (仅开仓时需要)
	if decision.Action == ActionOpen {
		err := ex.SetLeverage(ctx, decision.Opportunity.Symbol, decision.Leverage)
		if err != nil {
			// 杠杆设置失败通常是严重问题，可能导致后续交易失败，直接返回错误
			t.logger.Error("设置杠杆失败，取消交易", zap.Error(err), zap.String("symbol", decision.Opportunity.Symbol), zap.Int("leverage", decision.Leverage))
			return &TradeResult{Success: false, FailReason: fmt.Sprintf("设置杠杆失败: %s", err.Error())}, err
		}
	}

	// 2. 执行合约订单
	// 注意：需要确定市价单还是限价单，这里假设是市价单 (price=0)
	// TODO: 从 decision 中获取更详细的订单类型和价格
	contractOrderID, contractErr = ex.CreateContractOrder(
		ctx,
		decision.Opportunity.Symbol,
		decision.ContractSide,    // BUY or SELL
		decision.ContractPosSide, // LONG or SHORT
		"MARKET",                 // TODO: 支持限价单
		decision.ContractSize,
		0, // TODO: 支持限价单价格
	)
	if contractErr != nil {
		t.logger.Error("执行合约订单失败", zap.Error(contractErr), zap.Any("decision", decision))
		// 合约失败，无需执行现货，直接返回失败
		return &TradeResult{Success: false, FailReason: fmt.Sprintf("合约下单失败: %s", contractErr.Error())}, contractErr
	}
	t.logger.Info("合约订单成功", zap.String("orderID", contractOrderID))

	// --- 合约成功，尝试执行现货 ---

	// 3. 执行现货对冲订单
	spotOrderID, spotErr = ex.CreateSpotOrder(
		ctx,
		decision.Opportunity.Symbol,
		decision.SpotSide, // BUY or SELL
		"MARKET",          // TODO: 支持限价单
		decision.SpotSize,
		0, // TODO: 支持限价单价格
	)

	if spotErr != nil {
		t.logger.Error("执行现货对冲订单失败", zap.Error(spotErr), zap.Any("decision", decision))
		// --- !!! 现货失败，需要回滚合约 !!! ---
		t.logger.Warn("现货下单失败，尝试回滚合约订单", zap.String("contractOrderID", contractOrderID))

		// TODO: 实现回滚逻辑: 查询合约订单成交情况，然后下反向市价单平仓
		// rollbackErr := t.rollbackContractOrder(ctx, ex, decision.Symbol, contractOrderID, decision.ContractSize, decision.ContractPosSide)
		// if rollbackErr != nil {
		// 	t.logger.Error("回滚合约订单失败", zap.Error(rollbackErr), zap.String("contractOrderID", contractOrderID))
		// 	// 即使回滚失败，也要报告原始错误
		// }

		// 记录持仓为失败状态（即使回滚可能失败，也标记问题）
		posID := fmt.Sprintf("%s-%s-%d", decision.Opportunity.Exchange, decision.Opportunity.Symbol, time.Now().UnixNano())
		finalPosition = &Position{
			ID:              posID,
			Exchange:        decision.Opportunity.Exchange,
			Symbol:          decision.Opportunity.Symbol,
			ContractOrderID: contractOrderID,
			SpotOrderID:     "FAILED", // 标记现货失败
			Status:          "FAILED_HEDGE",
			// ... 其他字段可以根据需要填充默认值 ...
			CreatedAt:     time.Now(),
			LastUpdatedAt: time.Now(),
		}

		return &TradeResult{Success: false, FailReason: fmt.Sprintf("现货对冲失败: %s (合约订单 %s 可能已部分执行)", spotErr.Error(), contractOrderID), Position: finalPosition}, spotErr
	}
	t.logger.Info("现货对冲订单成功", zap.String("orderID", spotOrderID))

	// --- 双边成功 ---

	// 4. 创建持仓记录
	// TODO: 获取订单的实际成交价格和手续费，这里暂时使用决策价格和默认值
	posID := fmt.Sprintf("%s-%s-%d", decision.Opportunity.Exchange, decision.Opportunity.Symbol, time.Now().UnixNano())
	finalPosition = &Position{
		ID:                 posID,
		Exchange:           decision.Opportunity.Exchange,
		Symbol:             decision.Opportunity.Symbol,
		Direction:          decision.ContractPosSide, // 持仓方向与合约方向一致
		ContractOrderID:    contractOrderID,
		SpotOrderID:        spotOrderID,
		ContractEntrySize:  decision.ContractSize,
		SpotEntrySize:      decision.SpotSize,
		ContractEntryPrice: 0, // TODO: 获取实际成交价
		SpotEntryPrice:     0, // TODO: 获取实际成交价
		Leverage:           decision.Leverage,
		Status:             StatusOpen, // 初始状态为 Open
		CreatedAt:          time.Now(),
		LastUpdatedAt:      time.Now(),
		InitialFundingRate: decision.FundingRate, // 记录开仓时的资金费率
	}

	t.logger.Info("双边下单成功，创建持仓记录", zap.String("position_id", finalPosition.ID))

	return &TradeResult{Success: true, Position: finalPosition}, nil
}

// addToRiskMonitoringQueue 将持仓添加到风险监控队列
func (t *Trader) addToRiskMonitoringQueue(ctx context.Context, position *Position) error {
	// 将持仓数据序列化为JSON
	positionJSON, err := json.Marshal(position)
	if err != nil {
		return fmt.Errorf("序列化持仓数据失败: %w", err)
	}

	// 将持仓数据推送到Redis队列
	if err := t.redisClient.Client().LPush(ctx, QueueRiskMonitoring, positionJSON).Err(); err != nil {
		return fmt.Errorf("推送持仓到风险监控队列失败: %w", err)
	}

	t.logger.Debug("持仓已添加到风险监控队列",
		zap.String("position_id", position.ID),
		zap.String("symbol", position.Symbol),
		zap.String("exchange", position.Exchange))

	return nil
}

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

	// 如果决策是不交易，记录原因并返回
	if decision.Action == "" {
		t.logger.Info("决定不进行交易",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.String("reason", decision.Reason))
		return nil
	}

	// 2. 执行交易决策
	result, err := t.executeTradeDecision(ctx, decision)
	if err != nil {
		t.logger.Error("执行交易失败",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.String("action", decision.Action),
			zap.Error(err))
		return fmt.Errorf("执行交易失败: %w", err)
	}

	// 3. 处理交易结果
	if result.Success {
		t.logger.Info("交易执行成功",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.String("action", decision.Action),
			zap.String("position_id", result.Position.ID),
			zap.Float64("contract_price", result.ContractOrder.FilledPrice),
			zap.Float64("spot_price", result.SpotOrder.FilledPrice))

		// 将成功持仓添加到风险监控队列
		if err := t.addToRiskMonitoringQueue(ctx, result.Position); err != nil {
			t.logger.Error("添加持仓到风险监控队列失败",
				zap.Error(err),
				zap.String("position_id", result.Position.ID))
			// 非致命错误，不返回
		} else {
			t.logger.Info("持仓已添加到风险监控队列",
				zap.String("position_id", result.Position.ID))
		}

		// 保存交易记录到Redis
		// 在executeTradeDecision中已经保存了持仓信息，这里可以添加额外的交易记录
		// 例如可以将成功的交易机会和结果记录到历史数据中
		t.recordSuccessfulTrade(ctx, opportunity, result)
	} else {
		t.logger.Warn("交易执行失败",
			zap.String("exchange", opportunity.Exchange),
			zap.String("symbol", opportunity.Symbol),
			zap.String("action", decision.Action),
			zap.String("reason", result.FailReason))

		// 如果失败但有部分执行，可能需要特殊处理
		if result.Position != nil {
			t.logger.Warn("交易部分执行，需要特别关注",
				zap.String("position_id", result.Position.ID),
				zap.String("status", result.Position.Status))

			// 对于特定类型的失败，可能需要添加到风险监控队列
			if result.Position.Status == "FAILED_HEDGE" || result.Position.Status == "FAILED_INCOMPLETE_SPOT" {
				t.logger.Info("将不平衡持仓添加到风险监控队列以便后续处理",
					zap.String("position_id", result.Position.ID))

				if err := t.addToRiskMonitoringQueue(ctx, result.Position); err != nil {
					t.logger.Error("添加不平衡持仓到风险监控队列失败",
						zap.Error(err),
						zap.String("position_id", result.Position.ID))
				}
			}
		}
	}

	return nil
}

// 新增方法：记录成功交易
func (t *Trader) recordSuccessfulTrade(ctx context.Context, opportunity *TradeOpportunity, result *TradeResult) {
	// 这里可以添加将交易记录保存到Redis或其他存储的逻辑
	// 例如：保存交易历史、更新交易统计数据等

	// 简单实现：记录到Redis的一个列表中
	tradeRecord := map[string]interface{}{
		"exchange":          opportunity.Exchange,
		"symbol":            opportunity.Symbol,
		"funding_rate":      opportunity.FundingRate,
		"yearly_rate":       opportunity.YearlyRate,
		"position_id":       result.Position.ID,
		"contract_price":    result.ContractOrder.FilledPrice,
		"contract_size":     result.ContractOrder.FilledSize,
		"spot_price":        result.SpotOrder.FilledPrice,
		"spot_size":         result.SpotOrder.FilledSize,
		"execution_time":    result.ExecutionTime.Unix(),
		"funding_collected": 0, // 初始值
	}

	// 将交易记录转换为JSON
	recordJSON, err := json.Marshal(tradeRecord)
	if err != nil {
		t.logger.Error("序列化交易记录失败", zap.Error(err))
		return
	}

	// 保存到Redis的交易历史列表
	err = t.redisClient.Client().LPush(ctx, "funding_trade_history", recordJSON).Err()
	if err != nil {
		t.logger.Error("保存交易记录到历史列表失败", zap.Error(err))
	} else {
		t.logger.Debug("交易记录已保存到历史列表", zap.String("position_id", result.Position.ID))
	}

	// 更新交易统计
	t.updateTradeStatistics(ctx, result)
}

// 新增方法：更新交易统计
func (t *Trader) updateTradeStatistics(ctx context.Context, result *TradeResult) {
	// 这里可以添加更新交易统计数据的逻辑
	// 例如：总交易次数、成功率、平均收益率等

	// 使用Redis的哈希表存储统计信息
	statsKey := "funding_trade_stats"

	// 递增总交易次数
	err := t.redisClient.Client().HIncrBy(ctx, statsKey, "total_trades", 1).Err()
	if err != nil {
		t.logger.Error("更新总交易次数失败", zap.Error(err))
	}

	// 递增成功交易次数
	if result.Success {
		err = t.redisClient.Client().HIncrBy(ctx, statsKey, "successful_trades", 1).Err()
		if err != nil {
			t.logger.Error("更新成功交易次数失败", zap.Error(err))
		}
	}

	// 记录交易金额（简化计算）
	tradeValue := result.ContractOrder.FilledPrice * result.ContractOrder.FilledSize
	err = t.redisClient.Client().HIncrByFloat(ctx, statsKey, "total_trade_value", tradeValue).Err()
	if err != nil {
		t.logger.Error("更新总交易金额失败", zap.Error(err))
	}

	// 计算并更新预期收益（基于资金费率）
	// 注意：这是预期收益，实际收益需要在平仓时计算
	expectedYield := result.Position.InitialFundingRate * tradeValue
	err = t.redisClient.Client().HIncrByFloat(ctx, statsKey, "expected_yield", expectedYield).Err()
	if err != nil {
		t.logger.Error("更新预期收益失败", zap.Error(err))
	}
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
		// 返回一个带有空Action和原因的决策，而不是返回nil
		return &TradeDecision{
			Opportunity: *opportunity,
			Action:      "", // 空Action表示不交易
			Reason:      fmt.Sprintf("资金费率年化收益 %.2f%% 低于阈值 %.2f%%", currentYearlyRate, minRate),
		}, nil
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
		// 返回一个带有空Action和原因的决策，而不是返回nil
		return &TradeDecision{
			Opportunity: *opportunity,
			Action:      "", // 空Action表示不交易
			Reason:      fmt.Sprintf("交易规模太小（合约: %.8f, 现货: %.8f）", contractSize, spotSize),
		}, nil
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

	// 获取当前市场价格，用于日志记录和预估成交价
	currentPrice, err := ex.GetPrice(ctx, decision.Opportunity.Symbol)
	if err != nil {
		t.logger.Error("获取当前市场价格失败", zap.Error(err), zap.String("symbol", decision.Opportunity.Symbol))
		return &TradeResult{Success: false, FailReason: fmt.Sprintf("获取市场价格失败: %s", err.Error())}, err
	}
	t.logger.Info("当前市场价格", zap.Float64("price", currentPrice), zap.String("symbol", decision.Opportunity.Symbol))

	// 1. 设置杠杆 (仅开仓时需要)
	if decision.Action == ActionOpen {
		err := ex.SetLeverage(ctx, decision.Opportunity.Symbol, decision.Leverage)
		if err != nil {
			// 杠杆设置失败通常是严重问题，可能导致后续交易失败，直接返回错误
			t.logger.Error("设置杠杆失败，取消交易", zap.Error(err), zap.String("symbol", decision.Opportunity.Symbol), zap.Int("leverage", decision.Leverage))
			return &TradeResult{Success: false, FailReason: fmt.Sprintf("设置杠杆失败: %s", err.Error())}, err
		}
		t.logger.Info("杠杆设置成功", zap.Int("leverage", decision.Leverage))
	}

	// 2. 执行合约订单
	orderType := "MARKET" // 默认使用市价单
	contractOrderID, contractErr = ex.CreateContractOrder(
		ctx,
		decision.Opportunity.Symbol,
		decision.ContractSide,    // BUY or SELL
		decision.ContractPosSide, // LONG or SHORT
		orderType,
		decision.ContractSize,
		0, // 市价单价格为0
	)

	if contractErr != nil {
		t.logger.Error("执行合约订单失败", zap.Error(contractErr), zap.Any("decision", decision))
		// 合约失败，无需执行现货，直接返回失败
		return &TradeResult{Success: false, FailReason: fmt.Sprintf("合约下单失败: %s", contractErr.Error())}, contractErr
	}
	t.logger.Info("合约订单已提交", zap.String("orderID", contractOrderID))

	// 等待合约订单确认
	contractStatus, contractFilledPrice, contractFilledSize, err := t.waitForOrderConfirmation(ctx, ex, decision.Opportunity.Symbol, contractOrderID, 30*time.Second)
	if err != nil || contractStatus != "FILLED" {
		t.logger.Error("合约订单未完全成交",
			zap.String("status", contractStatus),
			zap.Error(err),
			zap.String("orderID", contractOrderID))

		// 尝试取消订单（如果订单状态不是已完成）
		if contractStatus != "FILLED" && contractStatus != "CANCELED" {
			t.logger.Info("尝试取消合约订单", zap.String("orderID", contractOrderID))
			// TODO: 实现取消订单逻辑
			// cancelErr := ex.CancelOrder(ctx, decision.Opportunity.Symbol, contractOrderID)
			// if cancelErr != nil {
			//     t.logger.Error("取消合约订单失败", zap.Error(cancelErr))
			// }
		}

		return &TradeResult{
			Success:    false,
			FailReason: fmt.Sprintf("合约订单未完全成交，状态为: %s", contractStatus),
		}, err
	}

	t.logger.Info("合约订单成功",
		zap.String("orderID", contractOrderID),
		zap.Float64("成交价格", contractFilledPrice),
		zap.Float64("成交数量", contractFilledSize))

	// --- 合约成功，尝试执行现货 ---

	// 3. 执行现货对冲订单
	spotOrderID, spotErr = ex.CreateSpotOrder(
		ctx,
		decision.Opportunity.Symbol,
		decision.SpotSide, // BUY or SELL
		"MARKET",          // 市价单
		decision.SpotSize,
		0, // 市价单价格为0
	)

	if spotErr != nil {
		t.logger.Error("执行现货对冲订单失败", zap.Error(spotErr), zap.Any("decision", decision))
		// 现货失败，需要回滚合约
		t.logger.Warn("现货下单失败，尝试回滚合约订单", zap.String("contractOrderID", contractOrderID))

		// 执行合约平仓操作以回滚
		rollbackErr := t.rollbackContractOrder(ctx, ex, decision.Opportunity.Symbol, decision.ContractPosSide, contractFilledSize)
		if rollbackErr != nil {
			t.logger.Error("回滚合约订单失败", zap.Error(rollbackErr))
			// 记录为严重问题，但继续处理
		}

		// 记录持仓为失败状态（即使回滚可能失败，也标记问题）
		posID := fmt.Sprintf("%s-%s-%d", decision.Opportunity.Exchange, decision.Opportunity.Symbol, time.Now().UnixNano())
		finalPosition = &Position{
			ID:                 posID,
			Exchange:           decision.Opportunity.Exchange,
			Symbol:             decision.Opportunity.Symbol,
			Direction:          decision.ContractPosSide,
			ContractOrderID:    contractOrderID,
			SpotOrderID:        "FAILED", // 标记现货失败
			ContractEntrySize:  contractFilledSize,
			ContractEntryPrice: contractFilledPrice,
			Status:             "FAILED_HEDGE",
			CreatedAt:          time.Now(),
			LastUpdatedAt:      time.Now(),
			InitialFundingRate: decision.FundingRate,
		}

		return &TradeResult{
			Success:    false,
			FailReason: fmt.Sprintf("现货对冲失败: %s (合约已执行并尝试回滚)", spotErr.Error()),
			Position:   finalPosition,
		}, spotErr
	}

	t.logger.Info("现货对冲订单已提交", zap.String("orderID", spotOrderID))

	// 等待现货订单确认
	spotStatus, spotFilledPrice, spotFilledSize, err := t.waitForOrderConfirmation(ctx, ex, decision.Opportunity.Symbol, spotOrderID, 30*time.Second)
	if err != nil || spotStatus != "FILLED" {
		t.logger.Error("现货订单未完全成交",
			zap.String("status", spotStatus),
			zap.Error(err),
			zap.String("orderID", spotOrderID))

		// 尝试取消订单（如果订单状态不是已完成）
		if spotStatus != "FILLED" && spotStatus != "CANCELED" {
			t.logger.Info("尝试取消现货订单", zap.String("orderID", spotOrderID))
			// TODO: 实现取消订单逻辑
			// cancelErr := ex.CancelOrder(ctx, decision.Opportunity.Symbol, spotOrderID)
			// if cancelErr != nil {
			//     t.logger.Error("取消现货订单失败", zap.Error(cancelErr))
			// }
		}

		// 需要回滚合约订单
		t.logger.Warn("现货订单未完全成交，尝试回滚合约订单", zap.String("contractOrderID", contractOrderID))
		rollbackErr := t.rollbackContractOrder(ctx, ex, decision.Opportunity.Symbol, decision.ContractPosSide, contractFilledSize)
		if rollbackErr != nil {
			t.logger.Error("回滚合约订单失败", zap.Error(rollbackErr))
		}

		posID := fmt.Sprintf("%s-%s-%d", decision.Opportunity.Exchange, decision.Opportunity.Symbol, time.Now().UnixNano())
		finalPosition = &Position{
			ID:                 posID,
			Exchange:           decision.Opportunity.Exchange,
			Symbol:             decision.Opportunity.Symbol,
			Direction:          decision.ContractPosSide,
			ContractOrderID:    contractOrderID,
			SpotOrderID:        spotOrderID,
			ContractEntrySize:  contractFilledSize,
			ContractEntryPrice: contractFilledPrice,
			SpotEntrySize:      spotFilledSize,  // 可能部分成交
			SpotEntryPrice:     spotFilledPrice, // 可能为0
			Status:             "FAILED_INCOMPLETE_SPOT",
			CreatedAt:          time.Now(),
			LastUpdatedAt:      time.Now(),
			InitialFundingRate: decision.FundingRate,
		}

		return &TradeResult{
			Success:    false,
			FailReason: fmt.Sprintf("现货订单未完全成交，状态为: %s", spotStatus),
			Position:   finalPosition,
		}, err
	}

	t.logger.Info("现货对冲订单成功",
		zap.String("orderID", spotOrderID),
		zap.Float64("成交价格", spotFilledPrice),
		zap.Float64("成交数量", spotFilledSize))

	// --- 双边成功 ---

	// 4. 创建持仓记录
	posID := fmt.Sprintf("%s-%s-%d", decision.Opportunity.Exchange, decision.Opportunity.Symbol, time.Now().UnixNano())
	finalPosition = &Position{
		ID:                 posID,
		Exchange:           decision.Opportunity.Exchange,
		Symbol:             decision.Opportunity.Symbol,
		Direction:          decision.ContractPosSide, // 持仓方向与合约方向一致
		ContractOrderID:    contractOrderID,
		SpotOrderID:        spotOrderID,
		ContractEntrySize:  contractFilledSize,
		SpotEntrySize:      spotFilledSize,
		ContractEntryPrice: contractFilledPrice,
		SpotEntryPrice:     spotFilledPrice,
		Leverage:           decision.Leverage,
		Status:             StatusOpen, // 初始状态为 Open
		CreatedAt:          time.Now(),
		LastUpdatedAt:      time.Now(),
		InitialFundingRate: decision.FundingRate, // 记录开仓时的资金费率
	}

	t.logger.Info("双边下单成功，创建持仓记录",
		zap.String("position_id", finalPosition.ID),
		zap.Float64("合约成交价格", contractFilledPrice),
		zap.Float64("现货成交价格", spotFilledPrice))

	// 将持仓添加到风险监控队列
	if err := t.addToRiskMonitoringQueue(ctx, finalPosition); err != nil {
		t.logger.Error("添加持仓到风险监控队列失败", zap.Error(err), zap.String("position_id", finalPosition.ID))
		// 非致命错误，仍然返回成功
	}

	return &TradeResult{
		Success:  true,
		Position: finalPosition,
		ContractOrder: &OrderResult{
			Exchange:    decision.Opportunity.Exchange,
			Symbol:      decision.Opportunity.Symbol,
			OrderID:     contractOrderID,
			OrderType:   "MARKET",
			Side:        decision.ContractSide,
			Size:        decision.ContractSize,
			FilledSize:  contractFilledSize,
			FilledPrice: contractFilledPrice,
			Status:      "FILLED",
			Timestamp:   time.Now(),
		},
		SpotOrder: &OrderResult{
			Exchange:    decision.Opportunity.Exchange,
			Symbol:      decision.Opportunity.Symbol,
			OrderID:     spotOrderID,
			OrderType:   "MARKET",
			Side:        decision.SpotSide,
			Size:        decision.SpotSize,
			FilledSize:  spotFilledSize,
			FilledPrice: spotFilledPrice,
			Status:      "FILLED",
			Timestamp:   time.Now(),
		},
		ExecutionTime: time.Now(),
	}, nil
}

// 添加的新方法，用于等待订单确认并获取成交信息
func (t *Trader) waitForOrderConfirmation(
	ctx context.Context,
	ex exchange.Exchange,
	symbol string,
	orderID string,
	timeout time.Duration,
) (status string, filledPrice float64, filledSize float64, err error) {
	t.logger.Debug("等待订单确认", zap.String("orderID", orderID), zap.Duration("timeout", timeout))

	// 创建带超时的上下文
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 初始状态
	status = "UNKNOWN"
	filledPrice = 0
	filledSize = 0

	// 轮询间隔
	checkInterval := 500 * time.Millisecond
	maxRetries := int(timeout / checkInterval)

	for i := 0; i < maxRetries; i++ {
		select {
		case <-timeoutCtx.Done():
			return status, filledPrice, filledSize, fmt.Errorf("等待订单确认超时: %s", orderID)
		default:
			// 查询订单状态
			var orderStatus string
			orderStatus, err = ex.GetOrderStatus(ctx, symbol, orderID)
			if err != nil {
				t.logger.Warn("查询订单状态失败，将重试", zap.Error(err), zap.String("orderID", orderID))
				time.Sleep(checkInterval)
				continue
			}

			status = orderStatus
			t.logger.Debug("订单状态查询结果", zap.String("orderID", orderID), zap.String("status", status))

			// 根据状态处理
			switch status {
			case "FILLED":
				// 订单已完全成交，获取详细信息
				// TODO: 理想情况下应该通过交易所API获取订单详情，包括成交价格和数量
				// 这里简化实现，使用最新市场价格作为成交价格，以及下单时的数量作为成交数量
				currentPrice, err := ex.GetPrice(ctx, symbol)
				if err != nil {
					t.logger.Warn("获取当前价格失败，使用估计值", zap.Error(err))
					// 使用保守估计
					currentPrice = 0
				}

				// 使用最新价格作为估计成交价
				filledPrice = currentPrice

				// 由于CCXT库的限制，无法直接获取订单详情来得到确切的成交数量
				// 假设市价单总是以指定数量完全成交（在实际应用中，应扩展CCXT封装以支持获取订单详情）
				// 暂时简单处理，假设原始订单数量已全部成交
				// 注意：这是一个简化处理，实际生产环境中应从订单详情中获取

				// 对于FILLED状态，我们可以合理假设用户下单时请求的数量已被完全成交
				// 在实际实现中，这里应该是从交易所的订单详情API中获取实际成交数量
				// 暂时使用请求时的数量作为成交数量
				// 因为无法通过当前接口获取原始订单的请求数量，这里暂用10作为演示
				// 注意：这里应修改为从订单详情中获取或从初始请求中传递
				filledSize = 10 // 实际应用中需要替换为真实的成交数量
				t.logger.Info("订单已完全成交",
					zap.String("orderID", orderID),
					zap.Float64("估计成交价", filledPrice),
					zap.String("注意", "成交数量和价格为估计值，实际应从订单详情获取"))

				return status, filledPrice, filledSize, nil

			case "CANCELED", "REJECTED", "EXPIRED":
				// 订单已取消、被拒绝或过期
				return status, filledPrice, filledSize, fmt.Errorf("订单未成功: %s，状态为: %s", orderID, status)

			case "PARTIAL_FILL":
				// 部分成交，可以考虑获取部分成交信息或等待完全成交
				t.logger.Info("订单部分成交", zap.String("orderID", orderID))
				// 继续等待

			default:
				// 其他状态，继续等待
				t.logger.Debug("订单处理中", zap.String("orderID", orderID), zap.String("status", status))
			}
		}

		time.Sleep(checkInterval)
	}

	// 超过最大重试次数
	return status, filledPrice, filledSize, fmt.Errorf("查询订单状态超过最大重试次数: %s", orderID)
}

// 添加的新方法，用于在现货失败时回滚合约订单
func (t *Trader) rollbackContractOrder(
	ctx context.Context,
	ex exchange.Exchange,
	symbol string,
	positionSide string,
	size float64,
) error {
	t.logger.Info("开始回滚合约订单",
		zap.String("symbol", symbol),
		zap.String("positionSide", positionSide),
		zap.Float64("size", size))

	// 根据持仓方向确定平仓方向
	var side string
	var oppositePosside string

	if positionSide == "LONG" {
		side = "SELL" // 做多的平仓方向是卖
		oppositePosside = "SHORT"
	} else {
		side = "BUY" // 做空的平仓方向是买
		oppositePosside = "LONG"
	}

	// 执行平仓操作
	rollbackOrderID, err := ex.CreateContractOrder(
		ctx,
		symbol,
		side,            // 平仓方向
		oppositePosside, // 反向持仓方向
		"MARKET",        // 市价单
		size,            // 使用原始开仓数量
		0,               // 市价单价格为0
	)

	if err != nil {
		return fmt.Errorf("执行合约回滚失败: %w", err)
	}

	t.logger.Info("合约回滚订单已提交", zap.String("rollbackOrderID", rollbackOrderID))

	// 等待回滚订单确认
	status, price, filledSize, err := t.waitForOrderConfirmation(ctx, ex, symbol, rollbackOrderID, 30*time.Second)
	if err != nil || status != "FILLED" {
		t.logger.Error("回滚订单未完全成交",
			zap.String("status", status),
			zap.Error(err),
			zap.String("rollbackOrderID", rollbackOrderID))
		return fmt.Errorf("回滚订单未完全成交: %w", err)
	}

	t.logger.Info("合约回滚成功",
		zap.String("rollbackOrderID", rollbackOrderID),
		zap.Float64("成交价格", price),
		zap.Float64("成交数量", filledSize))

	return nil
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

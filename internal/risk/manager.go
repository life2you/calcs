package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/life2you_mini/calcs/internal/config"
	"github.com/life2you_mini/calcs/internal/exchange"
	"github.com/life2you_mini/calcs/internal/storage"
	"github.com/life2you_mini/calcs/internal/trading" // 需要访问 PositionManager 和 Position 结构
)

const (
	// Redis 队列键名
	QueueRiskMonitoring = "risk_monitoring"

	// 任务处理超时
	riskTaskProcessTimeout = 15 * time.Second

	// 风险等级常量
	RiskLevelLow    = "LOW"
	RiskLevelMedium = "MEDIUM"
	RiskLevelHigh   = "HIGH"
)

// RiskThresholds 风险阈值配置
type RiskThresholds struct {
	// 清算风险阈值
	LowRiskLiqThreshold  float64 // 清算距离大于此值为低风险 (单位：%)
	MedRiskLiqThreshold  float64 // 清算距离大于此值为中风险 (单位：%)
	HighRiskLiqThreshold float64 // 清算距离低于此值为高风险 (单位：%)

	// 对冲偏差阈值
	LowRiskHedgeThreshold  float64 // 对冲偏差小于此值为低风险 (单位：%)
	MedRiskHedgeThreshold  float64 // 对冲偏差小于此值为中风险 (单位：%)
	HighRiskHedgeThreshold float64 // 对冲偏差高于此值为高风险 (单位：%)

	// 持仓时间阈值
	MaxHoldingTime time.Duration // 最大持仓时间
}

// 默认风险阈值
var defaultRiskThresholds = RiskThresholds{
	LowRiskLiqThreshold:  20.0, // 清算距离大于20%为低风险
	MedRiskLiqThreshold:  10.0, // 清算距离大于10%为中风险
	HighRiskLiqThreshold: 5.0,  // 清算距离低于5%为高风险

	LowRiskHedgeThreshold:  5.0,  // 对冲偏差小于5%为低风险
	MedRiskHedgeThreshold:  10.0, // 对冲偏差小于10%为中风险
	HighRiskHedgeThreshold: 15.0, // 对冲偏差高于15%为高风险

	MaxHoldingTime: 72 * time.Hour, // 最大持仓时间为72小时
}

// RiskManager 风险管理器
type RiskManager struct {
	ctx             context.Context
	cancel          context.CancelFunc
	logger          *zap.Logger
	config          *config.Config
	redisClient     *storage.RedisClient
	exchangeFactory *exchange.ExchangeFactory
	positionManager *trading.PositionManager
	thresholds      RiskThresholds
	wg              sync.WaitGroup
	isRunning       bool
	mutex           sync.Mutex
}

// NewRiskManager 创建新的风险管理器
func NewRiskManager(
	parentCtx context.Context,
	cfg *config.Config,
	logger *zap.Logger,
	redisClient *storage.RedisClient,
	exchangeFactory *exchange.ExchangeFactory,
	positionManager *trading.PositionManager,
) *RiskManager {
	ctx, cancel := context.WithCancel(parentCtx)

	// 使用默认风险阈值
	// TODO: 从配置中读取风险阈值
	thresholds := defaultRiskThresholds

	return &RiskManager{
		ctx:             ctx,
		cancel:          cancel,
		logger:          logger.With(zap.String("component", "risk_manager")),
		config:          cfg,
		redisClient:     redisClient,
		exchangeFactory: exchangeFactory,
		positionManager: positionManager,
		thresholds:      thresholds,
	}
}

// Start 启动风险管理器
func (rm *RiskManager) Start() error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if rm.isRunning {
		return fmt.Errorf("风险管理器已在运行")
	}

	rm.logger.Info("启动风险管理器")
	rm.isRunning = true

	// 启动风险监控队列处理协程
	rm.wg.Add(1)
	go rm.processRiskMonitoringQueue()

	return nil
}

// Stop 停止风险管理器
func (rm *RiskManager) Stop() error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if !rm.isRunning {
		return nil
	}

	rm.logger.Info("停止风险管理器")
	rm.cancel()

	// 等待所有协程结束
	done := make(chan struct{})
	go func() {
		rm.wg.Wait()
		close(done)
	}()

	// 等待最多5秒钟
	select {
	case <-done:
		rm.logger.Info("风险管理器已停止")
	case <-time.After(5 * time.Second):
		rm.logger.Warn("风险管理器停止超时")
	}

	rm.isRunning = false
	return nil
}

// processRiskMonitoringQueue 处理风险监控队列
func (rm *RiskManager) processRiskMonitoringQueue() {
	defer rm.wg.Done()

	rm.logger.Info("开始处理风险监控队列")

	for {
		// 检查上下文是否已取消
		select {
		case <-rm.ctx.Done():
			rm.logger.Info("结束风险监控处理")
			return
		default:
			// 继续处理
		}

		// 从队列获取任务 (这里假设队列中存储的是 Position 对象 JSON 字符串)
		// 注意：之前 trader.go 的 addToRiskMonitoringQueue 是模拟实现，需要调整为实际推入 Position JSON
		taskBytes, err := rm.redisClient.Client().BRPop(rm.ctx, 5*time.Second, QueueRiskMonitoring).Result()
		if err != nil {
			if err == redis.Nil {
				// 队列为空，正常情况，继续循环
				continue
			}
			rm.logger.Error("从风险监控队列获取任务失败", zap.Error(err))
			// 避免错误导致CPU空转
			time.Sleep(1 * time.Second)
			continue
		}

		// BRPop 返回的是 [queueName, value]
		if len(taskBytes) < 2 || taskBytes[1] == "" {
			continue
		}

		positionJSON := taskBytes[1]

		// 创建处理上下文
		processCtx, cancel := context.WithTimeout(rm.ctx, riskTaskProcessTimeout)

		// 处理风险任务
		if err := rm.handleRiskTask(processCtx, positionJSON); err != nil {
			rm.logger.Error("处理风险任务失败", zap.String("position_data", positionJSON), zap.Error(err))
		}

		cancel() // 释放上下文
	}
}

// handleRiskTask 处理单个风险监控任务
func (rm *RiskManager) handleRiskTask(ctx context.Context, positionJSON string) error {
	var position trading.Position
	if err := json.Unmarshal([]byte(positionJSON), &position); err != nil {
		return fmt.Errorf("反序列化持仓数据失败: %w", err)
	}

	rm.logger.Info("接收到风险监控任务", zap.String("position_id", position.ID))

	// 1. 获取交易所客户端
	ex, found := rm.exchangeFactory.Get(position.Exchange)
	if !found {
		rm.logger.Error("风险监控：找不到交易所客户端", zap.String("exchange", position.Exchange), zap.String("position_id", position.ID))
		// 无法处理，可能需要标记该持仓为错误状态或发送通知
		return fmt.Errorf("找不到交易所客户端: %s", position.Exchange)
	}

	// 2. 获取最新市场价格
	currentPrice, err := ex.GetPrice(ctx, position.Symbol)
	if err != nil {
		rm.logger.Warn("风险监控：获取最新价格失败", zap.Error(err), zap.String("symbol", position.Symbol), zap.String("position_id", position.ID))
		// 价格获取失败，暂时无法评估风险，可以稍后重试或发出警告
		return fmt.Errorf("获取最新价格失败: %w", err)
	}

	// 3. 计算清算风险
	// 获取交易所的维持保证金率（这里使用默认值，实际应从配置或交易所API获取）
	maintenanceMarginRate := 0.005 // 假设为0.5%

	liquidationPrice := CalculateLiquidationPrice(&position, maintenanceMarginRate)
	liquidationDistance := CalculateLiquidationDistance(currentPrice, liquidationPrice, position.Direction)

	// 4. 计算对冲偏差
	contractValue := CalculateContractValue(&position, currentPrice)
	spotValue := CalculateSpotValue(&position, currentPrice)
	hedgeImbalance := CalculateHedgeImbalance(contractValue, spotValue)

	// 5. 检查持仓时间
	holdingDuration := time.Since(position.CreatedAt)
	holdingTimeRisk := rm.evaluateHoldingTimeRisk(holdingDuration)

	// 6. 评估整体风险等级
	riskLevel := rm.evaluateOverallRisk(liquidationDistance, hedgeImbalance, holdingTimeRisk)

	rm.logger.Info("风险评估结果",
		zap.String("position_id", position.ID),
		zap.Float64("current_price", currentPrice),
		zap.Float64("liquidation_price", liquidationPrice),
		zap.Float64("liquidation_distance", liquidationDistance),
		zap.Float64("contract_value", contractValue),
		zap.Float64("spot_value", spotValue),
		zap.Float64("hedge_imbalance", hedgeImbalance),
		zap.Duration("holding_duration", holdingDuration),
		zap.String("holding_time_risk", holdingTimeRisk),
		zap.String("overall_risk_level", riskLevel))

	// 7. 根据风险等级执行相应操作
	var mitigationAction string
	var mitigationError error

	switch riskLevel {
	case RiskLevelHigh:
		// 高风险：可能需要平仓或其他紧急措施
		mitigationAction, mitigationError = rm.mitigateHighRisk(ctx, &position, liquidationDistance, hedgeImbalance, holdingDuration)
	case RiskLevelMedium:
		// 中等风险：可能需要调整仓位
		mitigationAction, mitigationError = rm.mitigateMediumRisk(ctx, &position, liquidationDistance, hedgeImbalance, holdingDuration)
	case RiskLevelLow:
		// 低风险：继续监控
		mitigationAction = "继续监控"
	}

	if mitigationError != nil {
		rm.logger.Error("风险缓解措施执行失败",
			zap.String("position_id", position.ID),
			zap.String("action", mitigationAction),
			zap.Error(mitigationError))
	} else if mitigationAction != "" {
		rm.logger.Info("执行风险缓解措施",
			zap.String("position_id", position.ID),
			zap.String("action", mitigationAction))
	}

	// 8. 更新持仓信息
	position.LastUpdatedAt = time.Now()
	position.LastRiskLevel = riskLevel
	position.LastRiskCheckTime = time.Now()

	if err := rm.positionManager.SavePosition(ctx, &position); err != nil {
		rm.logger.Error("风险监控：更新持仓信息失败", zap.Error(err), zap.String("position_id", position.ID))
		// 即使更新失败，也认为本次处理基本完成
	}

	return nil
}

// evaluateHoldingTimeRisk 评估持仓时间风险
func (rm *RiskManager) evaluateHoldingTimeRisk(holdingDuration time.Duration) string {
	maxTime := rm.thresholds.MaxHoldingTime

	// 持仓超过最大时间的80%为高风险
	if holdingDuration > maxTime*80/100 {
		return RiskLevelHigh
	}

	// 持仓超过最大时间的50%为中等风险
	if holdingDuration > maxTime*50/100 {
		return RiskLevelMedium
	}

	// 其他情况为低风险
	return RiskLevelLow
}

// evaluateOverallRisk 评估整体风险
func (rm *RiskManager) evaluateOverallRisk(liquidationDistance, hedgeImbalance float64, holdingTimeRisk string) string {
	// 评估清算风险和对冲风险
	riskLevel := EvaluateRiskLevel(
		liquidationDistance, hedgeImbalance,
		rm.thresholds.LowRiskLiqThreshold, rm.thresholds.MedRiskLiqThreshold,
		rm.thresholds.LowRiskHedgeThreshold, rm.thresholds.MedRiskHedgeThreshold,
	)

	// 取持仓时间风险和之前计算的风险中的最高级别
	if holdingTimeRisk == RiskLevelHigh || riskLevel == RiskLevelHigh {
		return RiskLevelHigh
	} else if holdingTimeRisk == RiskLevelMedium || riskLevel == RiskLevelMedium {
		return RiskLevelMedium
	} else {
		return RiskLevelLow
	}
}

// mitigateHighRisk 处理高风险情况
func (rm *RiskManager) mitigateHighRisk(ctx context.Context, position *trading.Position, liquidationDistance, hedgeImbalance float64, holdingDuration time.Duration) (string, error) {
	// 判断风险来源，决定采取的措施

	// 1. 清算风险过高，建议平仓
	if liquidationDistance < rm.thresholds.HighRiskLiqThreshold {
		rm.logger.Warn("清算风险过高，建议平仓",
			zap.String("position_id", position.ID),
			zap.Float64("liquidation_distance", liquidationDistance),
			zap.Float64("threshold", rm.thresholds.HighRiskLiqThreshold))

		// TODO: 实现自动平仓逻辑，或者推送平仓请求到交易队列
		// return rm.closePosition(ctx, position)
		return "建议平仓(清算风险)", nil
	}

	// 2. 对冲偏差过大，建议调整仓位
	if math.Abs(hedgeImbalance) > rm.thresholds.HighRiskHedgeThreshold {
		rm.logger.Warn("对冲偏差过大，建议调整仓位",
			zap.String("position_id", position.ID),
			zap.Float64("hedge_imbalance", hedgeImbalance),
			zap.Float64("threshold", rm.thresholds.HighRiskHedgeThreshold))

		// TODO: 实现仓位调整逻辑，或者推送调整请求到交易队列
		// return rm.rebalancePosition(ctx, position, hedgeImbalance)
		return "建议调整仓位(对冲偏差)", nil
	}

	// 3. 持仓时间过长，建议平仓
	if holdingDuration > rm.thresholds.MaxHoldingTime*80/100 {
		rm.logger.Warn("持仓时间过长，建议平仓",
			zap.String("position_id", position.ID),
			zap.Duration("holding_duration", holdingDuration),
			zap.Duration("max_duration", rm.thresholds.MaxHoldingTime))

		// TODO: 实现自动平仓逻辑，或者推送平仓请求到交易队列
		// return rm.closePosition(ctx, position)
		return "建议平仓(持仓时间)", nil
	}

	return "无高风险缓解措施", nil
}

// mitigateMediumRisk 处理中等风险情况
func (rm *RiskManager) mitigateMediumRisk(ctx context.Context, position *trading.Position, liquidationDistance, hedgeImbalance float64, holdingDuration time.Duration) (string, error) {
	// 1. 清算风险中等，可能需要调整杠杆或部分平仓
	if liquidationDistance < rm.thresholds.MedRiskLiqThreshold {
		rm.logger.Info("清算风险中等，考虑调整杠杆",
			zap.String("position_id", position.ID),
			zap.Float64("liquidation_distance", liquidationDistance),
			zap.Float64("threshold", rm.thresholds.MedRiskLiqThreshold))

		// TODO: 实现杠杆调整逻辑，或者生成调整建议
		// return rm.adjustLeverage(ctx, position)
		return "监控并考虑调整杠杆", nil
	}

	// 2. 对冲偏差中等，需要关注
	if math.Abs(hedgeImbalance) > rm.thresholds.MedRiskHedgeThreshold {
		rm.logger.Info("对冲偏差中等，需要关注",
			zap.String("position_id", position.ID),
			zap.Float64("hedge_imbalance", hedgeImbalance),
			zap.Float64("threshold", rm.thresholds.MedRiskHedgeThreshold))

		return "监控对冲偏差", nil
	}

	// 3. 持仓时间适中，需要关注
	if holdingDuration > rm.thresholds.MaxHoldingTime*50/100 {
		rm.logger.Info("持仓时间适中，需要关注",
			zap.String("position_id", position.ID),
			zap.Duration("holding_duration", holdingDuration),
			zap.Duration("max_duration", rm.thresholds.MaxHoldingTime))

		return "监控持仓时间", nil
	}

	return "常规监控", nil
}

// TODO: 实现平仓、调整杠杆等实际风险缓解操作
// func (rm *RiskManager) closePosition(ctx context.Context, position *trading.Position) (string, error) {...}
// func (rm *RiskManager) rebalancePosition(ctx context.Context, position *trading.Position, imbalance float64) (string, error) {...}
// func (rm *RiskManager) adjustLeverage(ctx context.Context, position *trading.Position) (string, error) {...}

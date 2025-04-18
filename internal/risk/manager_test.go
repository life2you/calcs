package risk

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/life2you_mini/calcs/internal/config"
	"github.com/life2you_mini/calcs/internal/exchange"
	"github.com/life2you_mini/calcs/internal/mocks"
	"github.com/life2you_mini/calcs/internal/trading"
)

func TestRiskManager_EvaluateHoldingTimeRisk(t *testing.T) {
	// 初始化测试日志
	logger := zaptest.NewLogger(t)

	// 创建测试用的风险管理器
	rm := &RiskManager{
		logger:     logger,
		thresholds: defaultRiskThresholds,
	}

	tests := []struct {
		name            string
		holdingDuration time.Duration
		expectedRisk    string
	}{
		{
			name:            "低风险-持仓时间短",
			holdingDuration: 10 * time.Hour, // 远小于阈值
			expectedRisk:    RiskLevelLow,
		},
		{
			name:            "中等风险-持仓时间中等",
			holdingDuration: 40 * time.Hour, // 大于MaxHoldingTime的50%
			expectedRisk:    RiskLevelMedium,
		},
		{
			name:            "高风险-持仓时间长",
			holdingDuration: 65 * time.Hour, // 大于MaxHoldingTime的80%
			expectedRisk:    RiskLevelHigh,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk := rm.evaluateHoldingTimeRisk(tt.holdingDuration)
			assert.Equal(t, tt.expectedRisk, risk)
		})
	}
}

func TestRiskManager_EvaluateOverallRisk(t *testing.T) {
	// 初始化测试日志
	logger := zaptest.NewLogger(t)

	// 创建测试用的风险管理器
	rm := &RiskManager{
		logger:     logger,
		thresholds: defaultRiskThresholds,
	}

	tests := []struct {
		name                string
		liquidationDistance float64
		hedgeImbalance      float64
		holdingTimeRisk     string
		expectedRisk        string
	}{
		{
			name:                "全部低风险",
			liquidationDistance: 25.0, // > LowRiskLiqThreshold
			hedgeImbalance:      2.0,  // < LowRiskHedgeThreshold
			holdingTimeRisk:     RiskLevelLow,
			expectedRisk:        RiskLevelLow,
		},
		{
			name:                "清算风险中等",
			liquidationDistance: 15.0, // < LowRiskLiqThreshold, > MedRiskLiqThreshold
			hedgeImbalance:      2.0,  // < LowRiskHedgeThreshold
			holdingTimeRisk:     RiskLevelLow,
			expectedRisk:        RiskLevelMedium,
		},
		{
			name:                "对冲风险高",
			liquidationDistance: 25.0, // > LowRiskLiqThreshold
			hedgeImbalance:      20.0, // > HighRiskHedgeThreshold
			holdingTimeRisk:     RiskLevelLow,
			expectedRisk:        RiskLevelHigh,
		},
		{
			name:                "持仓时间风险高",
			liquidationDistance: 25.0, // > LowRiskLiqThreshold
			hedgeImbalance:      2.0,  // < LowRiskHedgeThreshold
			holdingTimeRisk:     RiskLevelHigh,
			expectedRisk:        RiskLevelHigh,
		},
		{
			name:                "多种中等风险",
			liquidationDistance: 15.0, // < LowRiskLiqThreshold, > MedRiskLiqThreshold
			hedgeImbalance:      7.0,  // > LowRiskHedgeThreshold, < MedRiskHedgeThreshold
			holdingTimeRisk:     RiskLevelMedium,
			expectedRisk:        RiskLevelMedium,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk := rm.evaluateOverallRisk(tt.liquidationDistance, tt.hedgeImbalance, tt.holdingTimeRisk)
			assert.Equal(t, tt.expectedRisk, risk)
		})
	}
}

func TestRiskManager_HandleRiskTask(t *testing.T) {
	// 创建测试上下文
	ctx := context.Background()

	// 初始化测试日志
	logger := zaptest.NewLogger(t)

	// 创建模拟的交易所工厂
	mockExchangeFactory := new(mocks.ExchangeFactory)

	// 创建模拟的交易所
	mockExchange := new(mocks.Exchange)

	// 创建模拟的Redis客户端
	mockRedisClient := new(mocks.RedisClient)

	// 创建模拟的持仓管理器
	mockPositionManager := new(mocks.PositionManager)

	// 创建测试用的风险管理器
	rm := &RiskManager{
		ctx:             ctx,
		logger:          logger,
		config:          &config.Config{},
		exchangeFactory: mockExchangeFactory,
		redisClient:     mockRedisClient,
		positionManager: mockPositionManager,
		thresholds:      defaultRiskThresholds,
	}

	// 创建测试持仓
	testPosition := &trading.Position{
		ID:            "test-position-123",
		Exchange:      "binance",
		Symbol:        "BTC/USDT",
		Direction:     "long",
		EntryPrice:    40000.0,
		ContractSize:  1.0,
		SpotSize:      1.0,
		Leverage:      5,
		Status:        "OPEN",
		CreatedAt:     time.Now().Add(-24 * time.Hour), // 24小时前开仓
		LastUpdatedAt: time.Now().Add(-1 * time.Hour),  // 1小时前更新
	}

	// 序列化持仓数据
	positionJSON, _ := json.Marshal(testPosition)

	// 设置模拟预期行为
	mockExchangeFactory.On("Get", "binance").Return(mockExchange, true)
	mockExchange.On("GetPrice", mock.Anything, "BTC/USDT").Return(42000.0, nil)
	mockPositionManager.On("SavePosition", mock.Anything, mock.AnythingOfType("*trading.Position")).Return(nil)

	// 执行风险任务
	err := rm.handleRiskTask(ctx, string(positionJSON))

	// 验证结果
	assert.NoError(t, err)

	// 验证模拟调用
	mockExchangeFactory.AssertExpectations(t)
	mockExchange.AssertExpectations(t)
	mockPositionManager.AssertExpectations(t)
}

func TestCalculateLiquidationPrice(t *testing.T) {
	tests := []struct {
		name                  string
		position              *trading.Position
		maintenanceMarginRate float64
		expectedPrice         float64
	}{
		{
			name: "多仓-5倍杠杆",
			position: &trading.Position{
				EntryPrice: 40000.0,
				Leverage:   5,
				Direction:  "long",
			},
			maintenanceMarginRate: 0.005,   // 0.5%
			expectedPrice:         38000.0, // 40000 * (1 - 0.005 * 5) = 38000
		},
		{
			name: "空仓-10倍杠杆",
			position: &trading.Position{
				EntryPrice: 40000.0,
				Leverage:   10,
				Direction:  "short",
			},
			maintenanceMarginRate: 0.005,   // 0.5%
			expectedPrice:         42000.0, // 40000 * (1 + 0.005 * 10) = 42000
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price := CalculateLiquidationPrice(tt.position, tt.maintenanceMarginRate)
			assert.InDelta(t, tt.expectedPrice, price, 0.01) // 允许0.01的误差
		})
	}
}

func TestCalculateHedgeImbalance(t *testing.T) {
	tests := []struct {
		name              string
		contractValue     float64
		spotValue         float64
		expectedImbalance float64
	}{
		{
			name:              "平衡对冲",
			contractValue:     100000.0,
			spotValue:         100000.0,
			expectedImbalance: 0.0,
		},
		{
			name:              "合约价值大于现货",
			contractValue:     110000.0,
			spotValue:         100000.0,
			expectedImbalance: 10.0, // (110000 - 100000) / 100000 * 100 = 10%
		},
		{
			name:              "现货价值大于合约",
			contractValue:     90000.0,
			spotValue:         100000.0,
			expectedImbalance: -10.0, // (90000 - 100000) / 100000 * 100 = -10%
		},
		{
			name:              "现货价值为0",
			contractValue:     100000.0,
			spotValue:         0.0,
			expectedImbalance: 100.0, // 100%不平衡
		},
		{
			name:              "两者都为0",
			contractValue:     0.0,
			spotValue:         0.0,
			expectedImbalance: 0.0, // 视为平衡
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imbalance := CalculateHedgeImbalance(tt.contractValue, tt.spotValue)
			assert.InDelta(t, tt.expectedImbalance, imbalance, 0.01) // 允许0.01的误差
		})
	}
}

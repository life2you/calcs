package model

import (
	"time"
)

// TradeRecord 交易记录
type TradeRecord struct {
	ID            string    `json:"id"`
	PositionID    string    `json:"position_id"`
	Exchange      Exchange  `json:"exchange"`
	Symbol        string    `json:"symbol"`
	OrderType     string    `json:"order_type"` // CONTRACT_OPEN, CONTRACT_CLOSE, SPOT_BUY, SPOT_SELL
	OrderID       string    `json:"order_id"`
	Direction     string    `json:"direction"` // BUY, SELL
	Price         float64   `json:"price"`
	Size          float64   `json:"size"`
	Value         float64   `json:"value"`
	Fee           float64   `json:"fee"`
	ExecutionTime time.Time `json:"execution_time"`
	Status        string    `json:"status"` // SUCCESS, FAILED, PARTIAL
	ErrorMessage  string    `json:"error_message,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// AccountTransaction 账户资金变动记录
type AccountTransaction struct {
	ID                string    `json:"id"`
	Exchange          Exchange  `json:"exchange"`
	TransactionType   string    `json:"transaction_type"` // DEPOSIT, WITHDRAW, FUNDING_FEE, TRADE_FEE, PROFIT
	Asset             string    `json:"asset"`
	Amount            float64   `json:"amount"`
	Fee               float64   `json:"fee"`
	RelatedPositionID string    `json:"related_position_id,omitempty"`
	BalanceAfter      float64   `json:"balance_after"`
	Description       string    `json:"description,omitempty"`
	Timestamp         time.Time `json:"timestamp"`
}

// SystemLog 系统日志
type SystemLog struct {
	ID        string      `json:"id"`
	Level     string      `json:"level"`     // INFO, WARN, ERROR, FATAL
	Component string      `json:"component"` // MONITOR, TRADER, RISK, SYSTEM
	Message   string      `json:"message"`
	Metadata  interface{} `json:"metadata,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// RiskMetrics 风险指标
type RiskMetrics struct {
	ID                  string    `json:"id"`
	PositionID          string    `json:"position_id"`
	LiquidationDistance float64   `json:"liquidation_distance"`      // 清算距离百分比
	HedgeDeviation      float64   `json:"hedge_deviation"`           // 对冲偏差百分比
	VolatilityRisk      float64   `json:"volatility_risk"`           // 波动率风险得分
	LiquidityRisk       float64   `json:"liquidity_risk"`            // 流动性风险得分
	ExchangeRisk        float64   `json:"exchange_risk"`             // 交易所风险得分
	CompositeRiskScore  float64   `json:"composite_risk_score"`      // 综合风险评分
	RiskLevel           string    `json:"risk_level"`                // HIGH, MEDIUM, LOW
	Recommendations     []string  `json:"recommendations,omitempty"` // 风险缓解建议
	Timestamp           time.Time `json:"timestamp"`
}

// StoredConfig 配置存储
type StoredConfig struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Value     interface{} `json:"value"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// FundingPrediction 资金费率预测
type FundingPrediction struct {
	ID             string    `json:"id"`
	Exchange       Exchange  `json:"exchange"`
	Symbol         string    `json:"symbol"`
	CurrentRate    float64   `json:"current_rate"`
	PredictedRate  float64   `json:"predicted_rate"`
	Confidence     float64   `json:"confidence"`      // 预测置信度
	PredictionTime time.Time `json:"prediction_time"` // 预测时间点
	CreatedAt      time.Time `json:"created_at"`
}

package trading

import (
	"time"
)

// TradeOpportunity 代表一个交易机会
type TradeOpportunity struct {
	Exchange    string    `json:"exchange"`
	Symbol      string    `json:"symbol"`
	FundingRate float64   `json:"funding_rate"`
	YearlyRate  float64   `json:"yearly_rate"`
	Direction   string    `json:"direction"` // "LONG" 或 "SHORT"
	Timestamp   time.Time `json:"timestamp"`
}

// TradeDecision 代表交易决策
type TradeDecision struct {
	Opportunity     TradeOpportunity `json:"opportunity"`
	Action          string           `json:"action"`                // "OPEN", "CLOSE", "ADJUST"
	ContractSize    float64          `json:"contract_size"`         // 合约数量
	SpotSize        float64          `json:"spot_size"`             // 现货数量
	Leverage        int              `json:"leverage"`              // 杠杆倍数
	EstimatedProfit float64          `json:"estimated_profit"`      // 预计利润
	EntryPrice      float64          `json:"entry_price,omitempty"` // 执行价格
	Reason          string           `json:"reason"`                // 决策理由
	ContractSide    string           `json:"contract_side"`         // 合约交易方向，"BUY" 或 "SELL"
	ContractPosSide string           `json:"contract_pos_side"`     // 合约持仓方向，"LONG" 或 "SHORT"
	SpotSide        string           `json:"spot_side"`             // 现货交易方向，"BUY" 或 "SELL"
	FundingRate     float64          `json:"funding_rate"`          // 开仓时的资金费率
}

// Position 代表一个持仓 - 已移动到 position.go 文件中
/*
type Position struct {
	ID               string     `json:"id"`                          // 持仓ID
	Exchange         string     `json:"exchange"`                    // 交易所
	Symbol           string     `json:"symbol"`                      // 交易对
	Direction        string     `json:"direction"`                   // "LONG" 或 "SHORT"
	ContractSize     float64    `json:"contract_size"`               // 合约大小
	SpotSize         float64    `json:"spot_size"`                   // 现货大小
	EntryPrice       float64    `json:"entry_price"`                 // 入场价格
	Leverage         int        `json:"leverage"`                    // 杠杆倍数
	OpenTime         time.Time  `json:"open_time"`                   // 开仓时间
	LastUpdateTime   time.Time  `json:"last_update_time"`            // 最后更新时间
	ContractOrderID  string     `json:"contract_order_id,omitempty"` // 合约订单ID
	SpotOrderID      string     `json:"spot_order_id,omitempty"`     // 现货订单ID
	FundingCollected float64    `json:"funding_collected"`           // 已收取的资金费
	Status           string     `json:"status"`                      // "OPEN", "CLOSED", "ADJUSTING"
	CloseTime        *time.Time `json:"close_time,omitempty"`        // 平仓时间
	ClosePrice       *float64   `json:"close_price,omitempty"`       // 平仓价格
	PnL              *float64   `json:"pnl,omitempty"`               // 盈亏
	LiquidationPrice *float64   `json:"liquidation_price,omitempty"` // 预估清算价格
}
*/

// OrderResult 代表订单执行结果
type OrderResult struct {
	Exchange    string    `json:"exchange"`
	Symbol      string    `json:"symbol"`
	OrderID     string    `json:"order_id"`
	OrderType   string    `json:"order_type"` // "MARKET", "LIMIT"
	Side        string    `json:"side"`       // "BUY", "SELL"
	Size        float64   `json:"size"`
	Price       float64   `json:"price"`
	Status      string    `json:"status"` // "FILLED", "PARTIAL", "REJECTED"
	FilledSize  float64   `json:"filled_size"`
	FilledPrice float64   `json:"filled_price"`
	Message     string    `json:"message,omitempty"` // 错误消息
	Timestamp   time.Time `json:"timestamp"`
}

// TradeResult 代表交易执行结果
type TradeResult struct {
	Decision      TradeDecision `json:"decision"`
	ContractOrder *OrderResult  `json:"contract_order,omitempty"`
	SpotOrder     *OrderResult  `json:"spot_order,omitempty"`
	Position      *Position     `json:"position,omitempty"`
	Success       bool          `json:"success"`
	FailReason    string        `json:"fail_reason,omitempty"`
	ExecutionTime time.Time     `json:"execution_time"`
}

// PositionRisk 代表持仓风险评估
type PositionRisk struct {
	PositionID          string    `json:"position_id"`
	Exchange            string    `json:"exchange"`
	Symbol              string    `json:"symbol"`
	CurrentPrice        float64   `json:"current_price"`
	LiquidationPrice    float64   `json:"liquidation_price"`
	LiquidationDistance float64   `json:"liquidation_distance"` // 百分比
	HedgeDeviation      float64   `json:"hedge_deviation"`      // 对冲偏差百分比
	FundingRate         float64   `json:"funding_rate"`
	YearlyRate          float64   `json:"yearly_rate"`
	TimeHeld            float64   `json:"time_held"`          // 持有时间(小时)
	RiskScore           float64   `json:"risk_score"`         // 0-100, 越高风险越大
	RiskLevel           string    `json:"risk_level"`         // "LOW", "MEDIUM", "HIGH", "EXTREME"
	RecommendedAction   string    `json:"recommended_action"` // "HOLD", "ADJUST", "CLOSE"
	AssessmentTime      time.Time `json:"assessment_time"`
}

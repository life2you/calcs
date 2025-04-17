package models

import (
	"time"
)

// FundingRate 资金费率数据结构
type FundingRate struct {
	Exchange        string    `json:"exchange"`
	Symbol          string    `json:"symbol"`
	FundingRate     float64   `json:"funding_rate"`
	YearlyRate      float64   `json:"yearly_rate"`
	NextFundingTime time.Time `json:"next_funding_time"`
	Timestamp       time.Time `json:"timestamp"`
}

// TradingSignal 交易信号数据结构
type TradingSignal struct {
	Action            string    `json:"action"`             // 操作类型: OPEN_POSITION, CLOSE_POSITION
	Exchange          string    `json:"exchange"`           // 交易所
	Symbol            string    `json:"symbol"`             // 交易对
	FundingRate       float64   `json:"funding_rate"`       // 当前资金费率
	YearlyRate        float64   `json:"yearly_rate"`        // 年化收益率
	ContractDirection string    `json:"contract_direction"` // 合约方向: LONG, SHORT
	SpotDirection     string    `json:"spot_direction"`     // 现货方向: BUY, SELL
	SuggestedAmount   float64   `json:"suggested_amount"`   // 建议交易量
	Leverage          int       `json:"leverage"`           // 杠杆倍数
	Timestamp         time.Time `json:"timestamp"`          // 信号生成时间
	Score             float64   `json:"score"`              // 机会评分
}

// Position 持仓信息数据结构
type Position struct {
	PositionID       string    `json:"position_id"`       // 持仓ID
	Exchange         string    `json:"exchange"`          // 交易所
	Symbol           string    `json:"symbol"`            // 交易对
	ContractOrderID  string    `json:"contract_order_id"` // 合约订单ID
	SpotOrderID      string    `json:"spot_order_id"`     // 现货订单ID
	EntryPrice       float64   `json:"entry_price"`       // 入场价格
	LiquidationPrice float64   `json:"liquidation_price"` // 清算价格
	PositionSize     float64   `json:"position_size"`     // 持仓规模
	Leverage         int       `json:"leverage"`          // 杠杆倍数
	FundingCollected float64   `json:"funding_collected"` // 已收取资金费率总额
	CreationTime     time.Time `json:"creation_time"`     // 创建时间
	LastUpdateTime   time.Time `json:"last_update_time"`  // 最后更新时间
	Status           string    `json:"status"`            // 状态: ACTIVE, CLOSED
}

// RiskAlert 风险告警数据结构
type RiskAlert struct {
	PositionID       string    `json:"position_id"`       // 关联持仓ID
	AlertType        string    `json:"alert_type"`        // 告警类型: LIQUIDATION, HEDGE_IMBALANCE
	AlertLevel       string    `json:"alert_level"`       // 告警级别: LOW, MEDIUM, HIGH
	CurrentPrice     float64   `json:"current_price"`     // 当前价格
	LiquidationPrice float64   `json:"liquidation_price"` // 清算价格
	HedgeImbalance   float64   `json:"hedge_imbalance"`   // 对冲不平衡度
	Description      string    `json:"description"`       // 描述信息
	Timestamp        time.Time `json:"timestamp"`         // 告警时间
}

// Notification 通知数据结构
type Notification struct {
	Type      string    `json:"type"`       // 通知类型: TRADE, RISK, SYSTEM
	Priority  string    `json:"priority"`   // 优先级: LOW, MEDIUM, HIGH
	Message   string    `json:"message"`    // 通知内容
	RelatedID string    `json:"related_id"` // 关联ID(如持仓ID)
	Timestamp time.Time `json:"timestamp"`  // 通知时间
}

// Order 订单数据结构
type Order struct {
	Exchange     string    `json:"exchange"`      // 交易所
	OrderID      string    `json:"order_id"`      // 订单ID
	Symbol       string    `json:"symbol"`        // 交易对
	OrderType    string    `json:"order_type"`    // 订单类型: MARKET, LIMIT
	Direction    string    `json:"direction"`     // 方向: BUY, SELL
	Amount       float64   `json:"amount"`        // 数量
	Price        float64   `json:"price"`         // 价格(LIMIT订单)
	Status       string    `json:"status"`        // 状态: NEW, FILLED, CANCELED, REJECTED
	FilledPrice  float64   `json:"filled_price"`  // 成交价格
	FilledAmount float64   `json:"filled_amount"` // 成交数量
	CreateTime   time.Time `json:"create_time"`   // 创建时间
	UpdateTime   time.Time `json:"update_time"`   // 更新时间
	IsContract   bool      `json:"is_contract"`   // 是否为合约订单
}

// ExchangeBalance 交易所余额数据结构
type ExchangeBalance struct {
	Exchange   string             `json:"exchange"`    // 交易所
	UpdateTime time.Time          `json:"update_time"` // 更新时间
	Assets     map[string]float64 `json:"assets"`      // 资产余额: 币种 -> 数量
}

// FundingRateHistory 资金费率历史记录
type FundingRateHistory struct {
	Exchange    string              `json:"exchange"`     // 交易所
	Symbol      string              `json:"symbol"`       // 交易对
	StartTime   time.Time           `json:"start_time"`   // 开始时间
	EndTime     time.Time           `json:"end_time"`     // 结束时间
	Interval    string              `json:"interval"`     // 间隔
	RateRecords []FundingRateRecord `json:"rate_records"` // 费率记录
}

// FundingRateRecord 单条资金费率记录
type FundingRateRecord struct {
	Timestamp   time.Time `json:"timestamp"`    // 时间戳
	FundingRate float64   `json:"funding_rate"` // 资金费率
}

// ProfitRecord 盈利记录
type ProfitRecord struct {
	PositionID    string    `json:"position_id"`    // 持仓ID
	FundingProfit float64   `json:"funding_profit"` // 资金费率收益
	HedgingCost   float64   `json:"hedging_cost"`   // 对冲成本
	NetProfit     float64   `json:"net_profit"`     // 净收益
	HoldingTime   int64     `json:"holding_time"`   // 持有时间(秒)
	AnnualizedROI float64   `json:"annualized_roi"` // 年化收益率
	ClosingTime   time.Time `json:"closing_time"`   // 平仓时间
}

// SystemStats 系统统计信息
type SystemStats struct {
	TotalPositions   int       `json:"total_positions"`    // 总持仓数
	ActivePositions  int       `json:"active_positions"`   // 活跃持仓数
	TotalProfit      float64   `json:"total_profit"`       // 总盈利
	AvgAnnualizedROI float64   `json:"avg_annualized_roi"` // 平均年化收益率
	SuccessRate      float64   `json:"success_rate"`       // 成功率
	AvgHoldingTime   float64   `json:"avg_holding_time"`   // 平均持有时间(小时)
	LastUpdateTime   time.Time `json:"last_update_time"`   // 最后更新时间
}

package model

import (
	"time"
)

// Exchange 交易所类型
type Exchange string

// 支持的交易所
const (
	ExchangeBinance Exchange = "Binance"
	ExchangeOKX     Exchange = "OKX"
	ExchangeBitget  Exchange = "Bitget"
)

// FundingRateData 资金费率数据
type FundingRateData struct {
	Exchange        Exchange  `json:"exchange"`
	Symbol          string    `json:"symbol"`
	FundingRate     float64   `json:"funding_rate"`      // 当前资金费率
	YearlyRate      float64   `json:"yearly_rate"`       // 年化收益率(%)
	NextFundingTime time.Time `json:"next_funding_time"` // 下次结算时间
	Timestamp       time.Time `json:"timestamp"`         // 数据时间戳
}

// TradeSignal 交易信号
type TradeSignal struct {
	Action            string    `json:"action"` // OPEN_POSITION, CLOSE_POSITION
	Exchange          Exchange  `json:"exchange"`
	Symbol            string    `json:"symbol"`
	FundingRate       float64   `json:"funding_rate"`       // 当前资金费率
	YearlyRate        float64   `json:"yearly_rate"`        // 年化收益率
	ContractDirection string    `json:"contract_direction"` // "LONG" 或 "SHORT"
	SpotDirection     string    `json:"spot_direction"`     // "BUY" 或 "SELL"
	SuggestedAmount   float64   `json:"suggested_amount"`   // 建议交易数量
	Leverage          int       `json:"leverage"`           // 建议杠杆倍数
	Score             float64   `json:"score,omitempty"`    // 机会评分
	Timestamp         time.Time `json:"timestamp"`
}

// Position 持仓信息
type Position struct {
	PositionID       string    `json:"position_id"`
	Exchange         Exchange  `json:"exchange"`
	Symbol           string    `json:"symbol"`
	ContractOrderID  string    `json:"contract_order_id"`
	SpotOrderID      string    `json:"spot_order_id"`
	EntryPrice       float64   `json:"entry_price"`
	LiquidationPrice float64   `json:"liquidation_price"`
	PositionSize     float64   `json:"position_size"`
	Leverage         int       `json:"leverage"`
	FundingCollected float64   `json:"funding_collected"`
	CreationTime     time.Time `json:"creation_time"`
	Status           string    `json:"status"` // ACTIVE, CLOSED, PARTIAL_CLOSED
}

// RiskAlert 风险提醒
type RiskAlert struct {
	Type       string    `json:"type"` // LIQUIDATION_RISK, HEDGE_DEVIATION, FUNDING_RATE_CHANGE
	PositionID string    `json:"position_id"`
	Exchange   Exchange  `json:"exchange"`
	Symbol     string    `json:"symbol"`
	Severity   string    `json:"severity"` // HIGH, MEDIUM, LOW
	Message    string    `json:"message"`
	RiskValue  float64   `json:"risk_value"` // 相关风险值，如清算距离百分比
	Timestamp  time.Time `json:"timestamp"`
}

// NotificationMessage 通知消息
type NotificationMessage struct {
	Type      string      `json:"type"`     // TRADE, RISK, SYSTEM
	Priority  string      `json:"priority"` // HIGH, MEDIUM, LOW
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// MonitorTask 监控任务
type MonitorTask struct {
	Exchange     Exchange  `json:"exchange"`
	Symbol       string    `json:"symbol"`
	ScheduleTime time.Time `json:"schedule_time"`
	Priority     int       `json:"priority"`
}

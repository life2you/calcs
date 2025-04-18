package risk

import (
	"math"

	"github.com/life2you_mini/calcs/internal/trading"
)

// CalculateLiquidationPrice 计算仓位的清算价格
// 根据公式: LiqPrice = EntryPrice * (1 ± MMR * Leverage)
// 其中 + 用于空仓，- 用于多仓
func CalculateLiquidationPrice(position *trading.Position, maintenanceMarginRate float64) float64 {
	// 根据持仓方向使用不同公式
	if position.Direction == "long" {
		// 对于多仓，清算价格低于入场价
		return position.EntryPrice * (1 - maintenanceMarginRate*float64(position.Leverage))
	} else {
		// 对于空仓，清算价格高于入场价
		return position.EntryPrice * (1 + maintenanceMarginRate*float64(position.Leverage))
	}
}

// CalculateLiquidationDistance 计算当前价格距离清算价格的百分比距离
func CalculateLiquidationDistance(currentPrice, liquidationPrice float64, direction string) float64 {
	if direction == "long" {
		// 对于多仓，当前价格应高于清算价格
		return ((currentPrice - liquidationPrice) / currentPrice) * 100
	} else {
		// 对于空仓，当前价格应低于清算价格
		return ((liquidationPrice - currentPrice) / currentPrice) * 100
	}
}

// CalculateContractValue 计算合约仓位价值
func CalculateContractValue(position *trading.Position, currentPrice float64) float64 {
	contractValue := position.ContractSize * currentPrice

	// 注意：这里没有考虑PnL，如果需要精确计算，应该包含未实现盈亏
	// 未实现盈亏可以通过当前价格与入场价格之间的差异来计算
	return contractValue
}

// CalculateSpotValue 计算现货仓位价值
func CalculateSpotValue(position *trading.Position, currentPrice float64) float64 {
	spotValue := position.SpotSize * currentPrice
	return spotValue
}

// CalculateHedgeImbalance 计算对冲不平衡率
// 返回值为百分比形式的不平衡率，正值表示合约价值大于现货，负值表示现货价值大于合约
func CalculateHedgeImbalance(contractValue, spotValue float64) float64 {
	// 为避免除零错误
	if spotValue == 0 {
		if contractValue == 0 {
			return 0 // 两者都为0，视为平衡
		}
		return 100 // 只有合约没有现货，视为100%不平衡
	}

	// 计算不平衡率并转换为百分比
	imbalance := ((contractValue - spotValue) / spotValue) * 100
	return imbalance
}

// EvaluateRiskLevel 评估风险等级
// 根据清算距离和对冲不平衡率确定风险级别：LOW, MEDIUM, HIGH
func EvaluateRiskLevel(
	liquidationDistance, hedgeImbalance float64,
	lowRiskLiqThreshold, medRiskLiqThreshold float64,
	lowRiskHedgeThreshold, medRiskHedgeThreshold float64,
) string {
	// 取绝对值评估对冲不平衡
	hedgeImbalanceAbs := math.Abs(hedgeImbalance)

	// 清算风险评估
	var liqRiskLevel string
	if liquidationDistance > lowRiskLiqThreshold {
		liqRiskLevel = "LOW"
	} else if liquidationDistance > medRiskLiqThreshold {
		liqRiskLevel = "MEDIUM"
	} else {
		liqRiskLevel = "HIGH"
	}

	// 对冲风险评估
	var hedgeRiskLevel string
	if hedgeImbalanceAbs < lowRiskHedgeThreshold {
		hedgeRiskLevel = "LOW"
	} else if hedgeImbalanceAbs < medRiskHedgeThreshold {
		hedgeRiskLevel = "MEDIUM"
	} else {
		hedgeRiskLevel = "HIGH"
	}

	// 取两种风险中的最高级别
	if liqRiskLevel == "HIGH" || hedgeRiskLevel == "HIGH" {
		return "HIGH"
	} else if liqRiskLevel == "MEDIUM" || hedgeRiskLevel == "MEDIUM" {
		return "MEDIUM"
	} else {
		return "LOW"
	}
}

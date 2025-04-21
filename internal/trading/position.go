package trading

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"go.uber.org/zap"
)

// Position 持仓模型
type Position struct {
	ID                 string     `json:"id"`                    // 持仓ID
	Exchange           string     `json:"exchange"`              // 交易所
	Symbol             string     `json:"symbol"`                // 交易对
	Direction          string     `json:"direction"`             // 持仓方向 (LONG 或 SHORT)
	ContractOrderID    string     `json:"contract_order_id"`     // 合约订单ID
	SpotOrderID        string     `json:"spot_order_id"`         // 现货订单ID
	ContractEntrySize  float64    `json:"contract_entry_size"`   // 合约入场数量
	SpotEntrySize      float64    `json:"spot_entry_size"`       // 现货入场数量
	ContractEntryPrice float64    `json:"contract_entry_price"`  // 合约入场价格
	SpotEntryPrice     float64    `json:"spot_entry_price"`      // 现货入场价格
	ContractExitPrice  float64    `json:"contract_exit_price"`   // 合约平仓价格
	SpotExitPrice      float64    `json:"spot_exit_price"`       // 现货平仓价格
	ContractExitSize   float64    `json:"contract_exit_size"`    // 合约平仓数量
	SpotExitSize       float64    `json:"spot_exit_size"`        // 现货平仓数量
	Leverage           int        `json:"leverage"`              // 杠杆倍数
	Status             string     `json:"status"`                // 持仓状态
	CreatedAt          time.Time  `json:"created_at"`            // 创建时间
	LastUpdatedAt      time.Time  `json:"last_updated_at"`       // 最后更新时间
	LastRiskCheckTime  time.Time  `json:"last_risk_check_time"`  // 最后风险检查时间
	LastRiskLevel      string     `json:"last_risk_level"`       // 最后风险等级
	InitialFundingRate float64    `json:"initial_funding_rate"`  // 开仓时的资金费率
	TotalFundingFee    float64    `json:"total_funding_fee"`     // 累计资金费用
	PnL                float64    `json:"pnl"`                   // 当前盈亏
	RealizePnL         float64    `json:"realize_pnl"`           // 已实现盈亏
	CloseReason        string     `json:"close_reason"`          // 平仓原因
	CloseTime          *time.Time `json:"close_time,omitempty"`  // 平仓时间
	ClosePrice         *float64   `json:"close_price,omitempty"` // 平仓价格
}

// PositionManager 持仓管理器
type PositionManager struct {
	logger    *zap.Logger
	redisDB   *redis.Client
	keyPrefix string
}

// NewPositionManager 创建新的持仓管理器
func NewPositionManager(logger *zap.Logger, redisDB *redis.Client, keyPrefix string) *PositionManager {
	return &PositionManager{
		logger:    logger,
		redisDB:   redisDB,
		keyPrefix: keyPrefix,
	}
}

// CreatePosition 创建新持仓
func (pm *PositionManager) CreatePosition(ctx context.Context, position *Position) error {
	// 生成唯一ID
	if position.ID == "" {
		id, err := generateID()
		if err != nil {
			return fmt.Errorf("生成持仓ID失败: %w", err)
		}
		position.ID = id
	}

	// 设置时间戳
	now := time.Now()
	position.CreatedAt = now
	position.LastUpdatedAt = now
	position.Status = "OPEN"

	// 序列化并存储持仓
	return pm.SavePosition(ctx, position)
}

// GetPosition 获取持仓信息
func (pm *PositionManager) GetPosition(ctx context.Context, positionID string) (*Position, error) {
	key := pm.formatKey(positionID)
	positionJSON, err := pm.redisDB.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("持仓不存在: %s", positionID)
		}
		return nil, fmt.Errorf("获取持仓信息失败: %w", err)
	}

	var position Position
	if err := json.Unmarshal([]byte(positionJSON), &position); err != nil {
		return nil, fmt.Errorf("解析持仓数据失败: %w", err)
	}

	return &position, nil
}

// UpdatePosition 更新持仓信息
func (pm *PositionManager) UpdatePosition(ctx context.Context, position *Position) error {
	position.LastUpdatedAt = time.Now()
	return pm.SavePosition(ctx, position)
}

// ClosePosition 关闭持仓
func (pm *PositionManager) ClosePosition(ctx context.Context, positionID string, closePrice float64, pnl float64) error {
	position, err := pm.GetPosition(ctx, positionID)
	if err != nil {
		return err
	}

	now := time.Now()
	position.Status = "CLOSED"
	position.LastUpdatedAt = now
	position.CloseTime = &now
	position.ClosePrice = &closePrice
	position.PnL = pnl
	position.RealizePnL = pnl
	position.CloseReason = "Closed by user"

	return pm.SavePosition(ctx, position)
}

// ListOpenPositions 获取所有未平仓持仓的ID列表
func (pm *PositionManager) ListOpenPositions(ctx context.Context) ([]string, error) {
	// 从开仓持仓索引中获取
	key := pm.keyPrefix + "index:open_positions"
	return pm.redisDB.SMembers(ctx, key).Result()
}

// ListAllPositions 获取所有持仓的ID列表
func (pm *PositionManager) ListAllPositions(ctx context.Context) ([]string, error) {
	// 从全部持仓索引中获取
	key := pm.keyPrefix + "index:all_positions"
	return pm.redisDB.SMembers(ctx, key).Result()
}

// SavePosition 保存持仓信息
func (pm *PositionManager) SavePosition(ctx context.Context, position *Position) error {
	// 更新最后更新时间
	position.LastUpdatedAt = time.Now()

	// 序列化持仓数据
	positionJSON, err := json.Marshal(position)
	if err != nil {
		return fmt.Errorf("序列化持仓数据失败: %w", err)
	}

	// 保存到Redis
	key := pm.formatKey(position.ID)
	if err := pm.redisDB.Set(ctx, key, positionJSON, 0).Err(); err != nil {
		return fmt.Errorf("保存持仓数据失败: %w", err)
	}

	// 添加到持仓索引列表
	if err := pm.addPositionToIndex(ctx, position); err != nil {
		pm.logger.Warn("添加持仓到索引失败", zap.Error(err), zap.String("position_id", position.ID))
		// 不因为索引失败而影响主要的保存操作
	}

	return nil
}

// DeletePosition 删除持仓信息
func (pm *PositionManager) DeletePosition(ctx context.Context, positionID string) error {
	key := pm.formatKey(positionID)
	if err := pm.redisDB.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("删除持仓数据失败: %w", err)
	}

	// 从索引中移除
	if err := pm.removePositionFromIndex(ctx, positionID); err != nil {
		pm.logger.Warn("从索引移除持仓失败", zap.Error(err), zap.String("position_id", positionID))
		// 不因为索引失败而影响主要的删除操作
	}

	return nil
}

// formatKey 格式化Redis键名
func (pm *PositionManager) formatKey(positionID string) string {
	return pm.keyPrefix + "position:" + positionID
}

// addPositionToIndex 将持仓添加到索引
func (pm *PositionManager) addPositionToIndex(ctx context.Context, position *Position) error {
	// 添加到全部持仓索引
	allPositionsKey := pm.keyPrefix + "index:all_positions"
	if err := pm.redisDB.SAdd(ctx, allPositionsKey, position.ID).Err(); err != nil {
		return fmt.Errorf("添加到全部持仓索引失败: %w", err)
	}

	// 根据状态添加到对应索引
	if position.Status == "OPEN" {
		openPositionsKey := pm.keyPrefix + "index:open_positions"
		if err := pm.redisDB.SAdd(ctx, openPositionsKey, position.ID).Err(); err != nil {
			return fmt.Errorf("添加到开仓持仓索引失败: %w", err)
		}
	} else if position.Status == "CLOSED" {
		closedPositionsKey := pm.keyPrefix + "index:closed_positions"
		if err := pm.redisDB.SAdd(ctx, closedPositionsKey, position.ID).Err(); err != nil {
			return fmt.Errorf("添加到平仓持仓索引失败: %w", err)
		}

		// 如果是关闭状态，需要从开仓索引中移除
		openPositionsKey := pm.keyPrefix + "index:open_positions"
		pm.redisDB.SRem(ctx, openPositionsKey, position.ID)
	}

	return nil
}

// removePositionFromIndex 从索引中移除持仓
func (pm *PositionManager) removePositionFromIndex(ctx context.Context, positionID string) error {
	// 从所有索引中移除
	allPositionsKey := pm.keyPrefix + "index:all_positions"
	openPositionsKey := pm.keyPrefix + "index:open_positions"
	closedPositionsKey := pm.keyPrefix + "index:closed_positions"

	if err := pm.redisDB.SRem(ctx, allPositionsKey, positionID).Err(); err != nil {
		return fmt.Errorf("从全部持仓索引移除失败: %w", err)
	}

	pm.redisDB.SRem(ctx, openPositionsKey, positionID)
	pm.redisDB.SRem(ctx, closedPositionsKey, positionID)

	return nil
}

// generateID 生成唯一ID
func generateID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

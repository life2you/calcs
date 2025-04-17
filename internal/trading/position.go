package trading

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// PositionManager 负责管理持仓
type PositionManager struct {
	logger      *zap.Logger
	redisClient *redis.Client
	keyPrefix   string
}

// NewPositionManager 创建新的持仓管理器
func NewPositionManager(logger *zap.Logger, redisClient *redis.Client, keyPrefix string) *PositionManager {
	return &PositionManager{
		logger:      logger,
		redisClient: redisClient,
		keyPrefix:   keyPrefix,
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
	position.OpenTime = now
	position.LastUpdateTime = now
	position.Status = "OPEN"

	// 序列化并存储持仓
	return pm.savePosition(ctx, position)
}

// GetPosition 获取持仓详情
func (pm *PositionManager) GetPosition(ctx context.Context, positionID string) (*Position, error) {
	key := pm.getPositionKey(positionID)
	data, err := pm.redisClient.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("持仓不存在: %s", positionID)
	}

	// 反序列化
	position := &Position{}
	// TODO: 实现从Redis哈希表到结构体的映射
	// 这里简化实现，实际应使用适当的序列化方法

	return position, nil
}

// UpdatePosition 更新持仓信息
func (pm *PositionManager) UpdatePosition(ctx context.Context, position *Position) error {
	position.LastUpdateTime = time.Now()
	return pm.savePosition(ctx, position)
}

// ClosePosition 关闭持仓
func (pm *PositionManager) ClosePosition(ctx context.Context, positionID string, closePrice float64, pnl float64) error {
	position, err := pm.GetPosition(ctx, positionID)
	if err != nil {
		return err
	}

	now := time.Now()
	position.Status = "CLOSED"
	position.CloseTime = &now
	position.ClosePrice = &closePrice
	position.PnL = &pnl
	position.LastUpdateTime = now

	return pm.savePosition(ctx, position)
}

// ListOpenPositions 列出所有开放的持仓
func (pm *PositionManager) ListOpenPositions(ctx context.Context) ([]*Position, error) {
	// 获取所有开放持仓的ID
	openPositionsKey := fmt.Sprintf("%s:open_positions", pm.keyPrefix)
	positionIDs, err := pm.redisClient.SMembers(ctx, openPositionsKey).Result()
	if err != nil {
		return nil, err
	}

	if len(positionIDs) == 0 {
		return []*Position{}, nil
	}

	// 获取所有持仓详情
	positions := make([]*Position, 0, len(positionIDs))
	for _, id := range positionIDs {
		position, err := pm.GetPosition(ctx, id)
		if err != nil {
			pm.logger.Warn("获取持仓详情失败", zap.String("position_id", id), zap.Error(err))
			continue
		}
		positions = append(positions, position)
	}

	return positions, nil
}

// UpdateFundingCollected 更新已收取的资金费
func (pm *PositionManager) UpdateFundingCollected(ctx context.Context, positionID string, additionalFunding float64) error {
	position, err := pm.GetPosition(ctx, positionID)
	if err != nil {
		return err
	}

	position.FundingCollected += additionalFunding
	position.LastUpdateTime = time.Now()

	return pm.savePosition(ctx, position)
}

// 保存持仓到Redis
func (pm *PositionManager) savePosition(ctx context.Context, position *Position) error {
	// 获取持仓键
	key := pm.getPositionKey(position.ID)

	// 创建管道以执行多个操作
	pipe := pm.redisClient.Pipeline()

	// TODO: 实现结构体到Redis哈希表的映射
	// 这里简化实现，实际应使用适当的序列化方法
	// pipe.HSet(ctx, key, "id", position.ID)
	// pipe.HSet(ctx, key, "exchange", position.Exchange)
	// ...

	// 更新开放持仓集合
	openPositionsKey := fmt.Sprintf("%s:open_positions", pm.keyPrefix)
	if position.Status == "OPEN" {
		pipe.SAdd(ctx, openPositionsKey, position.ID)
	} else if position.Status == "CLOSED" {
		pipe.SRem(ctx, openPositionsKey, position.ID)
	}

	// 执行所有命令
	_, err := pipe.Exec(ctx)
	return err
}

// 获取持仓的Redis键
func (pm *PositionManager) getPositionKey(positionID string) string {
	return fmt.Sprintf("%s:position:%s", pm.keyPrefix, positionID)
}

// generateID 生成唯一ID
func generateID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

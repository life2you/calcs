package services

import (
	"context"
	"time"

	redisLib "github.com/redis/go-redis/v9"

	"github.com/life2you_mini/calcs/internal/exchange"
	"github.com/life2you_mini/calcs/internal/monitor"
	redisInternal "github.com/life2you_mini/calcs/internal/redis"
)

// RedisStorageAdapter 适配 redis.StorageClient 到 monitor.RedisClientInterface
type RedisStorageAdapter struct {
	storage *redisInternal.StorageClient
}

// NewRedisStorageAdapter 创建Redis存储适配器
func NewRedisStorageAdapter(storage *redisInternal.StorageClient) *RedisStorageAdapter {
	return &RedisStorageAdapter{
		storage: storage,
	}
}

// SaveFundingRate 实现 RedisClientInterface.SaveFundingRate
func (a *RedisStorageAdapter) SaveFundingRate(
	ctx context.Context,
	exchange, symbol string,
	rate, yearlyRate float64,
	timestamp time.Time,
) error {
	// 构建Redis键
	key := a.buildFundingRateKey(exchange, symbol)

	// 调用原始方法
	return a.storage.SaveFundingRate(ctx, key, rate, yearlyRate, timestamp)
}

// GetFundingRateHistory 实现 RedisClientInterface.GetFundingRateHistory
func (a *RedisStorageAdapter) GetFundingRateHistory(
	ctx context.Context,
	exchange, symbol string,
	startTime, endTime time.Time,
) ([]monitor.FundingRateData, error) {
	// 构建Redis键
	key := a.buildFundingRateKey(exchange, symbol)

	// 调用原始方法
	history, err := a.storage.GetFundingRateHistory(ctx, key, startTime, endTime)
	if err != nil {
		return nil, err
	}

	// 转换结果
	result := make([]monitor.FundingRateData, 0, len(history))
	for _, item := range history {
		rate, _ := item["rate"].(float64)
		yearlyRate, _ := item["yearly_rate"].(float64)
		ts, _ := item["timestamp"].(float64)

		result = append(result, monitor.FundingRateData{
			Exchange:    exchange,
			Symbol:      symbol,
			FundingRate: rate,
			YearlyRate:  yearlyRate,
			Timestamp:   time.Unix(int64(ts), 0),
		})
	}

	return result, nil
}

// GetPositionForRiskMonitoring 实现 RedisClientInterface.GetPositionForRiskMonitoring
func (a *RedisStorageAdapter) GetPositionForRiskMonitoring(ctx context.Context) ([]*exchange.Position, error) {
	// 这个实现需要根据实际情况编写
	// 返回空结果作为示例
	return []*exchange.Position{}, nil
}

// GetPositionForRiskMonitoringStr 实现 RedisClientInterface.GetPositionForRiskMonitoringStr
func (a *RedisStorageAdapter) GetPositionForRiskMonitoringStr(ctx context.Context) ([]string, error) {
	// 这个实现需要根据实际情况编写
	// 返回空结果作为示例
	return []string{}, nil
}

// SetLock 实现 RedisClientInterface.SetLock
func (a *RedisStorageAdapter) SetLock(
	ctx context.Context,
	key string,
	expiration time.Duration,
) (bool, error) {
	// 生成唯一值
	value := "1" // 简化实现，实际应使用唯一ID
	return a.storage.SetLock(ctx, key, value, expiration)
}

// ReleaseLock 实现 RedisClientInterface.ReleaseLock
func (a *RedisStorageAdapter) ReleaseLock(ctx context.Context, key string) error {
	// 简化实现，忽略返回值
	_, _ = a.storage.ReleaseLock(ctx, key, "1")
	return nil
}

// PushTask 实现 RedisClientInterface.PushTask
func (a *RedisStorageAdapter) PushTask(
	ctx context.Context,
	queue string,
	task interface{},
) error {
	return a.storage.PushTask(ctx, queue, task)
}

// PushTaskWithPriority 实现 RedisClientInterface.PushTaskWithPriority
func (a *RedisStorageAdapter) PushTaskWithPriority(
	ctx context.Context,
	queue string,
	task interface{},
	priority int,
) error {
	return a.storage.PushTaskWithPriority(ctx, queue, task, priority)
}

// Set 实现 RedisClientInterface.Set
func (a *RedisStorageAdapter) Set(
	ctx context.Context,
	key string,
	value interface{},
	expiration time.Duration,
) error {
	return a.storage.Set(ctx, key, value, expiration)
}

// Get 实现 RedisClientInterface.Get
func (a *RedisStorageAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.storage.Get(ctx, key)
}

// GetJSON 实现 RedisClientInterface.GetJSON
func (a *RedisStorageAdapter) GetJSON(ctx context.Context, key string, dest interface{}) error {
	return a.storage.GetJSON(ctx, key, dest)
}

// Client 实现 RedisClientInterface.Client
func (a *RedisStorageAdapter) Client() *redisLib.Client {
	return a.storage.GetClient()
}

// 构建资金费率键名
func (a *RedisStorageAdapter) buildFundingRateKey(exchange, symbol string) string {
	return "funding_rates:" + exchange + ":" + symbol
}

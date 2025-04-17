package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v8"
)

// 业务队列名称常量
const (
	FundingRateQueueKey = "funding:trade:queue"
	RiskMonitoringQueue = "funding:risk:queue"
	TradeHistoryKey     = "funding:trade:history"
)

// StorageClient Redis存储客户端封装
type StorageClient struct {
	client       *redis.Client
	queueService *QueueService
	keyPrefix    string
}

// NewStorageClient 创建新的Redis存储客户端
func NewStorageClient(addr string, password string, db int, keyPrefix string) (*StorageClient, error) {
	// 拆分地址和端口
	host, port := addr, ""
	if host == "" {
		host = "localhost:6379"
	}

	// 创建Redis客户端选项
	opts := ClientOptions{
		Host:     host,
		Port:     port,
		Password: password,
		DB:       db,
	}

	// 创建原始Redis客户端
	client, err := NewRedisClient(opts)
	if err != nil {
		return nil, fmt.Errorf("无法连接到Redis: %w", err)
	}

	// 创建队列服务
	queueService := NewQueueService(client, keyPrefix)

	return &StorageClient{
		client:       client,
		queueService: queueService,
		keyPrefix:    keyPrefix,
	}, nil
}

// GetClient 返回原始的Redis客户端
func (s *StorageClient) GetClient() *redis.Client {
	return s.client
}

// GetQueueService 返回队列服务
func (s *StorageClient) GetQueueService() *QueueService {
	return s.queueService
}

// Close 关闭Redis连接
func (s *StorageClient) Close() error {
	return s.client.Close()
}

// SaveFundingRate 保存资金费率数据
func (s *StorageClient) SaveFundingRate(
	ctx context.Context,
	key string,
	rate float64,
	yearlyRate float64,
	timestamp time.Time,
) error {
	// 使用时间戳作为score，数据作为成员添加到有序集合
	data := map[string]interface{}{
		"rate":        rate,
		"yearly_rate": yearlyRate,
		"timestamp":   timestamp.Unix(),
	}

	// 将数据序列化为JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化资金费率数据失败: %w", err)
	}

	// 添加到有序集合，使用时间戳作为分数
	score := float64(timestamp.Unix())
	err = s.client.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: string(jsonData),
	}).Err()

	if err != nil {
		return fmt.Errorf("保存资金费率数据到Redis失败: %w", err)
	}

	// 移除旧数据（保留最近7天数据）
	oldScore := float64(timestamp.AddDate(0, 0, -7).Unix())
	err = s.client.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%f", oldScore)).Err()
	if err != nil {
		return fmt.Errorf("清理旧资金费率数据失败: %w", err)
	}

	return nil
}

// GetFundingRateHistory 获取资金费率历史数据
func (s *StorageClient) GetFundingRateHistory(
	ctx context.Context,
	key string,
	start, end time.Time,
) ([]map[string]interface{}, error) {
	// 获取指定时间范围内的资金费率数据
	startScore := float64(start.Unix())
	endScore := float64(end.Unix())

	// 从有序集合获取数据
	results, err := s.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%f", startScore),
		Max: fmt.Sprintf("%f", endScore),
	}).Result()

	if err != nil {
		return nil, fmt.Errorf("获取资金费率历史数据失败: %w", err)
	}

	// 解析结果
	var history []map[string]interface{}
	for _, jsonStr := range results {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			return nil, fmt.Errorf("解析资金费率历史数据失败: %w", err)
		}
		history = append(history, data)
	}

	return history, nil
}

// AddToTradeQueue 添加交易机会到队列
func (s *StorageClient) AddToTradeQueue(ctx context.Context, opportunity interface{}) error {
	// 使用队列服务的PushTask方法
	return s.queueService.PushTask(ctx, QueueTradeOpportunities, opportunity)
}

// PopFromTradeQueue 从交易队列弹出一个交易机会
func (s *StorageClient) PopFromTradeQueue(ctx context.Context, timeout time.Duration) ([]byte, error) {
	// 使用队列服务的PopTask方法
	return s.queueService.PopTask(ctx, QueueTradeOpportunities, timeout)
}

// GetTradeOpportunity 从队列获取交易机会（返回字符串格式）
func (s *StorageClient) GetTradeOpportunity(ctx context.Context, timeout time.Duration) (string, error) {
	data, err := s.PopFromTradeQueue(ctx, timeout)
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", nil
	}
	return string(data), nil
}

// SaveTradeHistory 保存交易历史
func (s *StorageClient) SaveTradeHistory(ctx context.Context, tradeData interface{}) error {
	// 序列化交易数据
	jsonData, err := json.Marshal(tradeData)
	if err != nil {
		return fmt.Errorf("序列化交易数据失败: %w", err)
	}

	// 添加到Redis列表
	err = s.client.LPush(ctx, s.keyPrefix+TradeHistoryKey, string(jsonData)).Err()
	if err != nil {
		return fmt.Errorf("保存交易历史失败: %w", err)
	}

	// 只保留最近1000条交易记录
	err = s.client.LTrim(ctx, s.keyPrefix+TradeHistoryKey, 0, 999).Err()
	if err != nil {
		return fmt.Errorf("裁剪交易历史失败: %w", err)
	}

	return nil
}

// AddToRiskMonitoringQueue 添加到风险监控队列
func (s *StorageClient) AddToRiskMonitoringQueue(ctx context.Context, position interface{}) error {
	// 使用队列服务的PushTask方法
	return s.queueService.PushTask(ctx, QueueRiskMonitoring, position)
}

// GetPositionForRiskMonitoring 从风险监控队列获取持仓
func (s *StorageClient) GetPositionForRiskMonitoring(ctx context.Context, timeout time.Duration) ([]byte, error) {
	// 使用队列服务的PopTask方法
	return s.queueService.PopTask(ctx, QueueRiskMonitoring, timeout)
}

// GetPositionForRiskMonitoringStr 从风险监控队列获取持仓（字符串格式）
func (s *StorageClient) GetPositionForRiskMonitoringStr(ctx context.Context, timeout time.Duration) (string, error) {
	data, err := s.GetPositionForRiskMonitoring(ctx, timeout)
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", nil
	}
	return string(data), nil
}

// SetLock 设置分布式锁
func (s *StorageClient) SetLock(ctx context.Context, lockKey string, value string, expiration time.Duration) (bool, error) {
	// 使用CreateLock方法
	return CreateLock(s.client, lockKey, value, expiration)
}

// ReleaseLock 释放分布式锁
func (s *StorageClient) ReleaseLock(ctx context.Context, lockKey string, value string) (bool, error) {
	// 使用ReleaseLock方法
	return ReleaseLock(s.client, lockKey, value)
}

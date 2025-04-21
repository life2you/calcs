package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/life2you_mini/calcs/internal/model"
	"go.uber.org/zap"
)

// Redis 键前缀常量
const (
	// 资金费率相关
	keyFundingRatePrefix  = "funding:rate:"
	keyFundingRateAll     = "funding:rate:all"
	keyFundingRateHistory = "funding:rate:history:"

	// 持仓相关
	keyPositionPrefix       = "position:"
	keyPositionStatusPrefix = "position:status:"
	keyPositionIDs          = "position:ids"

	// 交易记录相关
	keyTradePrefix      = "trade:"
	keyTradesByPosition = "trade:position:"

	// 账户交易相关
	keyAccountTxPrefix = "account:tx:"
	keyAccountTxByType = "account:tx:type:"

	// 系统日志相关
	keySystemLogPrefix  = "system:log:"
	keySystemLogByLevel = "system:log:level:"

	// 风险指标相关
	keyRiskMetricsPrefix     = "risk:metrics:"
	keyRiskMetricsByPosition = "risk:metrics:position:"

	// 过期时间（秒）
	expiryFundingRate = 86400 * 30  // 30天
	expiryPosition    = 86400 * 180 // 180天
	expiryTrade       = 86400 * 365 // 365天
	expiryAccountTx   = 86400 * 365 // 365天
	expirySystemLog   = 86400 * 90  // 90天
	expiryRiskMetrics = 86400 * 90  // 90天
)

// RedisStorage Redis存储实现
type RedisStorage struct {
	client *redis.Client
	logger *zap.Logger
}

// NewRedisStorage 创建Redis存储
func NewRedisStorage(client *redis.Client, logger *zap.Logger) *RedisStorage {
	return &RedisStorage{
		client: client,
		logger: logger,
	}
}

// Initialize 初始化Redis存储
func (s *RedisStorage) Initialize(ctx context.Context) error {
	// 测试连接
	if err := s.client.Ping(ctx).Err(); err != nil {
		s.logger.Error("Redis连接失败", zap.Error(err))
		return fmt.Errorf("redis连接失败: %w", err)
	}

	s.logger.Info("Redis存储初始化成功")
	return nil
}

// Close 关闭Redis连接
func (s *RedisStorage) Close(ctx context.Context) error {
	if err := s.client.Close(); err != nil {
		s.logger.Error("关闭Redis连接失败", zap.Error(err))
		return fmt.Errorf("关闭Redis连接失败: %w", err)
	}

	s.logger.Info("Redis连接已关闭")
	return nil
}

// Health 检查Redis健康状态
func (s *RedisStorage) Health(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// StoreFundingRate 存储资金费率数据
func (s *RedisStorage) StoreFundingRate(ctx context.Context, data *model.FundingRateData) error {
	// 将资金费率数据序列化为JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化资金费率数据失败: %w", err)
	}

	// 生成键
	key := keyFundingRatePrefix + string(data.Exchange) + ":" + data.Symbol
	historyKey := keyFundingRateHistory + string(data.Exchange) + ":" + data.Symbol
	timestamp := strconv.FormatInt(data.Timestamp.Unix(), 10)

	// 使用Pipeline批量执行
	pipe := s.client.Pipeline()

	// 存储最新资金费率
	pipe.Set(ctx, key, jsonData, time.Duration(expiryFundingRate)*time.Second)

	// 存储历史资金费率（使用有序集合，按时间戳排序）
	pipe.ZAdd(ctx, historyKey, redis.Z{
		Score:  float64(data.Timestamp.Unix()),
		Member: jsonData,
	})

	// 设置历史数据过期时间
	pipe.Expire(ctx, historyKey, time.Duration(expiryFundingRate)*time.Second)

	// 添加到全局集合中
	globalKey := keyFundingRateAll
	pipe.HSet(ctx, globalKey, timestamp+":"+string(data.Exchange)+":"+data.Symbol, jsonData)
	pipe.Expire(ctx, globalKey, time.Duration(expiryFundingRate)*time.Second)

	// 执行Pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("存储资金费率数据失败: %w", err)
	}

	return nil
}

// GetFundingRates 获取指定交易所和交易对的资金费率数据
func (s *RedisStorage) GetFundingRates(ctx context.Context, exchange, symbol string, limit int) ([]*model.FundingRateData, error) {
	historyKey := keyFundingRateHistory + exchange + ":" + symbol

	// 从有序集合中获取最近的N条记录
	results, err := s.client.ZRevRangeWithScores(ctx, historyKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("获取资金费率历史数据失败: %w", err)
	}

	rates := make([]*model.FundingRateData, 0, len(results))
	for _, result := range results {
		jsonData, ok := result.Member.(string)
		if !ok {
			s.logger.Warn("资金费率数据类型错误", zap.Any("result", result))
			continue
		}

		var rate model.FundingRateData
		if err := json.Unmarshal([]byte(jsonData), &rate); err != nil {
			s.logger.Warn("解析资金费率数据失败", zap.Error(err), zap.String("data", jsonData))
			continue
		}

		rates = append(rates, &rate)
	}

	return rates, nil
}

// GetAllFundingRates 获取所有交易所和交易对的最新资金费率
func (s *RedisStorage) GetAllFundingRates(ctx context.Context, limit int) ([]*model.FundingRateData, error) {
	globalKey := keyFundingRateAll

	// 获取所有资金费率数据
	results, err := s.client.HGetAll(ctx, globalKey).Result()
	if err != nil {
		return nil, fmt.Errorf("获取所有资金费率数据失败: %w", err)
	}

	rates := make([]*model.FundingRateData, 0, len(results))
	for _, jsonData := range results {
		var rate model.FundingRateData
		if err := json.Unmarshal([]byte(jsonData), &rate); err != nil {
			s.logger.Warn("解析资金费率数据失败", zap.Error(err), zap.String("data", jsonData))
			continue
		}

		rates = append(rates, &rate)
	}

	// 如果有限制，只返回最新的N条
	if limit > 0 && len(rates) > limit {
		// 按时间戳排序（倒序）
		// 这里应该实现排序逻辑，但为简化代码，先返回前N条
		rates = rates[:limit]
	}

	return rates, nil
}

// GetAverageFundingRate 获取平均资金费率
func (s *RedisStorage) GetAverageFundingRate(ctx context.Context, exchange, symbol string, period time.Duration) (float64, error) {
	historyKey := keyFundingRateHistory + exchange + ":" + symbol

	// 计算时间范围
	now := time.Now()
	minTime := now.Add(-period).Unix()

	// 从有序集合中获取指定时间范围内的记录
	results, err := s.client.ZRangeByScore(ctx, historyKey, &redis.ZRangeBy{
		Min: strconv.FormatInt(minTime, 10),
		Max: strconv.FormatInt(now.Unix(), 10),
	}).Result()
	if err != nil {
		return 0, fmt.Errorf("获取资金费率历史数据失败: %w", err)
	}

	if len(results) == 0 {
		return 0, nil
	}

	// 计算平均值
	var sum float64
	count := 0

	for _, jsonData := range results {
		var rate model.FundingRateData
		if err := json.Unmarshal([]byte(jsonData), &rate); err != nil {
			s.logger.Warn("解析资金费率数据失败", zap.Error(err), zap.String("data", jsonData))
			continue
		}

		sum += rate.FundingRate
		count++
	}

	if count == 0 {
		return 0, nil
	}

	return sum / float64(count), nil
}

// StorePosition 存储持仓信息
func (s *RedisStorage) StorePosition(ctx context.Context, position *model.Position) error {
	// 将持仓数据序列化为JSON
	jsonData, err := json.Marshal(position)
	if err != nil {
		return fmt.Errorf("序列化持仓数据失败: %w", err)
	}

	// 生成键
	key := keyPositionPrefix + position.PositionID
	statusKey := keyPositionStatusPrefix + position.Status

	// 使用Pipeline批量执行
	pipe := s.client.Pipeline()

	// 存储持仓数据
	pipe.Set(ctx, key, jsonData, time.Duration(expiryPosition)*time.Second)

	// 将持仓ID添加到状态集合中
	pipe.SAdd(ctx, statusKey, position.PositionID)
	pipe.Expire(ctx, statusKey, time.Duration(expiryPosition)*time.Second)

	// 将持仓ID添加到全局集合中
	pipe.SAdd(ctx, keyPositionIDs, position.PositionID)

	// 执行Pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("存储持仓数据失败: %w", err)
	}

	return nil
}

// GetPositions 获取指定状态的持仓列表
func (s *RedisStorage) GetPositions(ctx context.Context, status string) ([]*model.Position, error) {
	statusKey := keyPositionStatusPrefix + status

	// 获取该状态下的所有持仓ID
	positionIDs, err := s.client.SMembers(ctx, statusKey).Result()
	if err != nil {
		return nil, fmt.Errorf("获取持仓ID列表失败: %w", err)
	}

	if len(positionIDs) == 0 {
		return []*model.Position{}, nil
	}

	// 获取所有持仓数据
	positions := make([]*model.Position, 0, len(positionIDs))
	for _, id := range positionIDs {
		position, err := s.GetPositionByID(ctx, id)
		if err != nil {
			s.logger.Warn("获取持仓数据失败", zap.Error(err), zap.String("position_id", id))
			continue
		}

		positions = append(positions, position)
	}

	return positions, nil
}

// GetPositionByID 根据ID获取持仓信息
func (s *RedisStorage) GetPositionByID(ctx context.Context, positionID string) (*model.Position, error) {
	key := keyPositionPrefix + positionID

	// 获取持仓数据
	jsonData, err := s.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("持仓不存在: %s", positionID)
		}
		return nil, fmt.Errorf("获取持仓数据失败: %w", err)
	}

	// 解析持仓数据
	var position model.Position
	if err := json.Unmarshal([]byte(jsonData), &position); err != nil {
		return nil, fmt.Errorf("解析持仓数据失败: %w", err)
	}

	return &position, nil
}

// UpdatePosition 更新持仓信息
func (s *RedisStorage) UpdatePosition(ctx context.Context, position *model.Position) error {
	// 获取当前持仓信息
	currentPosition, err := s.GetPositionByID(ctx, position.PositionID)
	if err != nil {
		return fmt.Errorf("获取当前持仓信息失败: %w", err)
	}

	// 如果状态发生变化，需要更新状态集合
	if currentPosition.Status != position.Status {
		pipe := s.client.Pipeline()

		// 从旧状态集合中移除
		oldStatusKey := keyPositionStatusPrefix + currentPosition.Status
		pipe.SRem(ctx, oldStatusKey, position.PositionID)

		// 添加到新状态集合中
		newStatusKey := keyPositionStatusPrefix + position.Status
		pipe.SAdd(ctx, newStatusKey, position.PositionID)
		pipe.Expire(ctx, newStatusKey, time.Duration(expiryPosition)*time.Second)

		// 执行Pipeline
		_, err = pipe.Exec(ctx)
		if err != nil {
			return fmt.Errorf("更新持仓状态失败: %w", err)
		}
	}

	// 更新持仓信息
	return s.StorePosition(ctx, position)
}

// StoreTradeRecord 存储交易记录
func (s *RedisStorage) StoreTradeRecord(ctx context.Context, trade *model.TradeRecord) error {
	// 将交易记录序列化为JSON
	jsonData, err := json.Marshal(trade)
	if err != nil {
		return fmt.Errorf("序列化交易记录失败: %w", err)
	}

	// 生成键
	key := keyTradePrefix + trade.ID
	positionKey := keyTradesByPosition + trade.PositionID

	// 使用Pipeline批量执行
	pipe := s.client.Pipeline()

	// 存储交易记录
	pipe.Set(ctx, key, jsonData, time.Duration(expiryTrade)*time.Second)

	// 将交易ID添加到持仓交易集合中
	pipe.SAdd(ctx, positionKey, trade.ID)
	pipe.Expire(ctx, positionKey, time.Duration(expiryTrade)*time.Second)

	// 执行Pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("存储交易记录失败: %w", err)
	}

	return nil
}

// GetTradeRecords 获取指定持仓的交易记录
func (s *RedisStorage) GetTradeRecords(ctx context.Context, positionID string) ([]*model.TradeRecord, error) {
	positionKey := keyTradesByPosition + positionID

	// 获取该持仓下的所有交易ID
	tradeIDs, err := s.client.SMembers(ctx, positionKey).Result()
	if err != nil {
		return nil, fmt.Errorf("获取交易ID列表失败: %w", err)
	}

	if len(tradeIDs) == 0 {
		return []*model.TradeRecord{}, nil
	}

	// 获取所有交易记录
	trades := make([]*model.TradeRecord, 0, len(tradeIDs))
	for _, id := range tradeIDs {
		key := keyTradePrefix + id

		// 获取交易记录
		jsonData, err := s.client.Get(ctx, key).Result()
		if err != nil {
			if err != redis.Nil {
				s.logger.Warn("获取交易记录失败", zap.Error(err), zap.String("trade_id", id))
			}
			continue
		}

		// 解析交易记录
		var trade model.TradeRecord
		if err := json.Unmarshal([]byte(jsonData), &trade); err != nil {
			s.logger.Warn("解析交易记录失败", zap.Error(err), zap.String("data", jsonData))
			continue
		}

		trades = append(trades, &trade)
	}

	return trades, nil
}

// StoreAccountTransaction 存储账户交易记录
func (s *RedisStorage) StoreAccountTransaction(ctx context.Context, transaction *model.AccountTransaction) error {
	// 将交易记录序列化为JSON
	jsonData, err := json.Marshal(transaction)
	if err != nil {
		return fmt.Errorf("序列化账户交易记录失败: %w", err)
	}

	// 生成键
	key := keyAccountTxPrefix + transaction.ID
	typeKey := keyAccountTxByType + string(transaction.TransactionType)

	// 使用Pipeline批量执行
	pipe := s.client.Pipeline()

	// 存储交易记录
	pipe.Set(ctx, key, jsonData, time.Duration(expiryAccountTx)*time.Second)

	// 将交易ID添加到类型集合中
	pipe.ZAdd(ctx, typeKey, redis.Z{
		Score:  float64(transaction.Timestamp.Unix()),
		Member: transaction.ID,
	})
	pipe.Expire(ctx, typeKey, time.Duration(expiryAccountTx)*time.Second)

	// 执行Pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("存储账户交易记录失败: %w", err)
	}

	return nil
}

// GetAccountTransactions 获取指定时间范围内的账户交易记录
func (s *RedisStorage) GetAccountTransactions(ctx context.Context, start, end time.Time) ([]*model.AccountTransaction, error) {
	// 获取所有交易类型
	keys, err := s.client.Keys(ctx, keyAccountTxByType+"*").Result()
	if err != nil {
		return nil, fmt.Errorf("获取交易类型列表失败: %w", err)
	}

	if len(keys) == 0 {
		return []*model.AccountTransaction{}, nil
	}

	// 获取指定时间范围内的所有交易记录
	startScore := float64(start.Unix())
	endScore := float64(end.Unix())

	transactions := make([]*model.AccountTransaction, 0)
	for _, typeKey := range keys {
		// 获取该类型下指定时间范围内的所有交易ID
		ids, err := s.client.ZRangeByScore(ctx, typeKey, &redis.ZRangeBy{
			Min: strconv.FormatFloat(startScore, 'f', 0, 64),
			Max: strconv.FormatFloat(endScore, 'f', 0, 64),
		}).Result()
		if err != nil {
			s.logger.Warn("获取账户交易ID列表失败", zap.Error(err), zap.String("type_key", typeKey))
			continue
		}

		// 获取所有交易记录
		for _, id := range ids {
			key := keyAccountTxPrefix + id

			// 获取交易记录
			jsonData, err := s.client.Get(ctx, key).Result()
			if err != nil {
				if err != redis.Nil {
					s.logger.Warn("获取账户交易记录失败", zap.Error(err), zap.String("tx_id", id))
				}
				continue
			}

			// 解析交易记录
			var tx model.AccountTransaction
			if err := json.Unmarshal([]byte(jsonData), &tx); err != nil {
				s.logger.Warn("解析账户交易记录失败", zap.Error(err), zap.String("data", jsonData))
				continue
			}

			transactions = append(transactions, &tx)
		}
	}

	return transactions, nil
}

// StoreSystemLog 存储系统日志
func (s *RedisStorage) StoreSystemLog(ctx context.Context, log *model.SystemLog) error {
	// 将日志序列化为JSON
	jsonData, err := json.Marshal(log)
	if err != nil {
		return fmt.Errorf("序列化系统日志失败: %w", err)
	}

	// 生成键
	key := keySystemLogPrefix + log.ID
	levelKey := keySystemLogByLevel + log.Level

	// 使用Pipeline批量执行
	pipe := s.client.Pipeline()

	// 存储日志
	pipe.Set(ctx, key, jsonData, time.Duration(expirySystemLog)*time.Second)

	// 将日志ID添加到级别集合中
	pipe.ZAdd(ctx, levelKey, redis.Z{
		Score:  float64(log.Timestamp.Unix()),
		Member: log.ID,
	})
	pipe.Expire(ctx, levelKey, time.Duration(expirySystemLog)*time.Second)

	// 执行Pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("存储系统日志失败: %w", err)
	}

	return nil
}

// GetSystemLogs 获取指定级别的系统日志
func (s *RedisStorage) GetSystemLogs(ctx context.Context, level string, limit int) ([]*model.SystemLog, error) {
	levelKey := keySystemLogByLevel + level

	// 获取该级别下的最近N条日志ID
	ids, err := s.client.ZRevRange(ctx, levelKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("获取系统日志ID列表失败: %w", err)
	}

	if len(ids) == 0 {
		return []*model.SystemLog{}, nil
	}

	// 获取所有日志
	logs := make([]*model.SystemLog, 0, len(ids))
	for _, id := range ids {
		key := keySystemLogPrefix + id

		// 获取日志
		jsonData, err := s.client.Get(ctx, key).Result()
		if err != nil {
			if err != redis.Nil {
				s.logger.Warn("获取系统日志失败", zap.Error(err), zap.String("log_id", id))
			}
			continue
		}

		// 解析日志
		var log model.SystemLog
		if err := json.Unmarshal([]byte(jsonData), &log); err != nil {
			s.logger.Warn("解析系统日志失败", zap.Error(err), zap.String("data", jsonData))
			continue
		}

		logs = append(logs, &log)
	}

	return logs, nil
}

// StoreRiskMetrics 存储风险指标
func (s *RedisStorage) StoreRiskMetrics(ctx context.Context, metrics *model.RiskMetrics) error {
	// 将风险指标序列化为JSON
	jsonData, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("序列化风险指标失败: %w", err)
	}

	// 生成键
	key := keyRiskMetricsPrefix + metrics.ID
	positionKey := keyRiskMetricsByPosition + metrics.PositionID

	// 使用Pipeline批量执行
	pipe := s.client.Pipeline()

	// 存储风险指标
	pipe.Set(ctx, key, jsonData, time.Duration(expiryRiskMetrics)*time.Second)

	// 将风险指标ID添加到持仓风险指标集合中
	pipe.ZAdd(ctx, positionKey, redis.Z{
		Score:  float64(metrics.Timestamp.Unix()),
		Member: metrics.ID,
	})
	pipe.Expire(ctx, positionKey, time.Duration(expiryRiskMetrics)*time.Second)

	// 执行Pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("存储风险指标失败: %w", err)
	}

	return nil
}

// GetLatestRiskMetrics 获取最新风险指标
func (s *RedisStorage) GetLatestRiskMetrics(ctx context.Context, positionID string) (*model.RiskMetrics, error) {
	positionKey := keyRiskMetricsByPosition + positionID

	// 获取该持仓下的最新风险指标ID
	ids, err := s.client.ZRevRange(ctx, positionKey, 0, 0).Result()
	if err != nil {
		return nil, fmt.Errorf("获取风险指标ID列表失败: %w", err)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("未找到风险指标: %s", positionID)
	}

	key := keyRiskMetricsPrefix + ids[0]

	// 获取风险指标
	jsonData, err := s.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("风险指标不存在: %s", ids[0])
		}
		return nil, fmt.Errorf("获取风险指标失败: %w", err)
	}

	// 解析风险指标
	var metrics model.RiskMetrics
	if err := json.Unmarshal([]byte(jsonData), &metrics); err != nil {
		return nil, fmt.Errorf("解析风险指标失败: %w", err)
	}

	return &metrics, nil
}

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"time"
)

// 队列常量
const (
	QueueMonitorTasks       = "monitor_tasks"
	QueueTradeOpportunities = "trade_opportunities"
	QueueRiskMonitoring     = "risk_monitoring"
	QueueNotifications      = "notifications"

	PriorityQueuePrefix = "priority_"
	DelayedQueuePrefix  = "delayed_"
)

// QueueService Redis队列服务
type QueueService struct {
	client    *redis.Client
	keyPrefix string
}

// NewQueueService 创建新的队列服务
func NewQueueService(client *redis.Client, keyPrefix string) *QueueService {
	return &QueueService{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// 获取完整的队列名称
func (q *QueueService) getQueueKey(queue string) string {
	return fmt.Sprintf("%s%s", q.keyPrefix, queue)
}

// 获取优先级队列名称
func (q *QueueService) getPriorityQueueKey(queue string) string {
	return fmt.Sprintf("%s%s%s", q.keyPrefix, PriorityQueuePrefix, queue)
}

// 获取延迟队列名称
func (q *QueueService) getDelayedQueueKey(queue string) string {
	return fmt.Sprintf("%s%s%s", q.keyPrefix, DelayedQueuePrefix, queue)
}

// PushTask 将任务推送到队列
func (q *QueueService) PushTask(ctx context.Context, queue string, task interface{}) error {
	taskData, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("序列化任务失败: %w", err)
	}

	queueKey := q.getQueueKey(queue)
	return q.client.LPush(ctx, queueKey, taskData).Err()
}

// PopTask 从队列中弹出任务（阻塞方式）
func (q *QueueService) PopTask(ctx context.Context, queue string, timeout time.Duration) ([]byte, error) {
	queueKey := q.getQueueKey(queue)
	result, err := q.client.BRPop(ctx, timeout, queueKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // 超时
		}
		return nil, err
	}

	// BRPop返回一个包含两个元素的数组：[queueName, value]
	if len(result) < 2 {
		return nil, fmt.Errorf("从队列获取的数据结构不正确")
	}

	return []byte(result[1]), nil
}

// GetQueueLength 获取队列长度
func (q *QueueService) GetQueueLength(ctx context.Context, queue string) (int64, error) {
	queueKey := q.getQueueKey(queue)
	return q.client.LLen(ctx, queueKey).Result()
}

// ClearQueue 清空队列
func (q *QueueService) ClearQueue(ctx context.Context, queue string) error {
	queueKey := q.getQueueKey(queue)
	return q.client.Del(ctx, queueKey).Err()
}

// PushTaskWithPriority 将任务推送到优先级队列
func (q *QueueService) PushTaskWithPriority(ctx context.Context, queue string, task interface{}, priority float64) error {
	taskData, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("序列化任务失败: %w", err)
	}

	priorityQueueKey := q.getPriorityQueueKey(queue)
	return q.client.ZAdd(ctx, priorityQueueKey, redis.Z{
		Score:  priority,
		Member: taskData,
	}).Err()
}

// PopTaskWithPriority 从优先级队列中弹出最高优先级的任务
func (q *QueueService) PopTaskWithPriority(ctx context.Context, queue string) ([]byte, error) {
	priorityQueueKey := q.getPriorityQueueKey(queue)

	// 使用事务确保获取和删除操作的原子性
	txf := func(tx *redis.Tx) error {
		// 获取最高优先级的任务
		tasks, err := tx.ZRevRangeWithScores(ctx, priorityQueueKey, 0, 0).Result()
		if err != nil {
			return err
		}

		if len(tasks) == 0 {
			return redis.Nil
		}

		// 删除该任务
		_, err = tx.ZRem(ctx, priorityQueueKey, tasks[0].Member).Result()
		return err
	}

	// 执行事务
	for i := 0; i < 3; i++ { // 尝试3次
		err := q.client.Watch(ctx, txf, priorityQueueKey)
		if err == nil {
			// 获取最高优先级的任务
			tasks, err := q.client.ZRevRangeWithScores(ctx, priorityQueueKey, 0, 0).Result()
			if err != nil {
				return nil, err
			}

			if len(tasks) == 0 {
				return nil, redis.Nil
			}

			// 删除该任务
			_, err = q.client.ZRem(ctx, priorityQueueKey, tasks[0].Member).Result()
			if err != nil {
				return nil, err
			}

			return []byte(tasks[0].Member.(string)), nil
		}

		if err == redis.TxFailedErr {
			continue
		}

		return nil, err
	}

	return nil, fmt.Errorf("无法从优先级队列中弹出任务")
}

// PushDelayedTask 推送延迟任务
func (q *QueueService) PushDelayedTask(ctx context.Context, queue string, task interface{}, delay time.Duration) error {
	taskData, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("序列化任务失败: %w", err)
	}

	// 计算执行时间
	executeAt := float64(time.Now().Add(delay).Unix())
	delayedQueueKey := q.getDelayedQueueKey(queue)

	return q.client.ZAdd(ctx, delayedQueueKey, redis.Z{
		Score:  executeAt,
		Member: taskData,
	}).Err()
}

// GetReadyDelayedTasks 获取准备好执行的延迟任务
func (q *QueueService) GetReadyDelayedTasks(ctx context.Context, queue string) ([][]byte, error) {
	delayedQueueKey := q.getDelayedQueueKey(queue)
	now := float64(time.Now().Unix())

	// 获取所有准备好执行的任务（得分小于等于当前时间戳的任务）
	tasks, err := q.client.ZRangeByScore(ctx, delayedQueueKey, &redis.ZRangeBy{
		Min: "0",
		Max: fmt.Sprintf("%f", now),
	}).Result()
	if err != nil {
		return nil, err
	}

	if len(tasks) == 0 {
		return [][]byte{}, nil
	}

	// 从延迟队列中删除这些任务
	_, err = q.client.ZRemRangeByScore(ctx, delayedQueueKey, "0", fmt.Sprintf("%f", now)).Result()
	if err != nil {
		return nil, err
	}

	// 转换结果
	result := make([][]byte, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, []byte(task))
	}

	return result, nil
}

// MoveReadyTasksToQueue 将准备好的延迟任务移动到常规队列
func (q *QueueService) MoveReadyTasksToQueue(ctx context.Context, delayedQueue, targetQueue string) (int, error) {
	tasks, err := q.GetReadyDelayedTasks(ctx, delayedQueue)
	if err != nil {
		return 0, err
	}

	if len(tasks) == 0 {
		return 0, nil
	}

	targetQueueKey := q.getQueueKey(targetQueue)
	pipe := q.client.Pipeline()

	// 将所有任务添加到目标队列
	for _, task := range tasks {
		pipe.LPush(ctx, targetQueueKey, task)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}

	return len(tasks), nil
}

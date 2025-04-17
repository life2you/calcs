package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v8"
)

// ClientOptions Redis客户端配置选项
type ClientOptions struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// NewRedisClient 创建新的Redis客户端
func NewRedisClient(opts ClientOptions) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         opts.Host + ":" + opts.Port,
		Password:     opts.Password,
		DB:           opts.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
		MaxRetries:   3,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return client, nil
}

// CreateLock 创建一个分布式锁
func CreateLock(client *redis.Client, key string, value string, ttl time.Duration) (bool, error) {
	ctx := context.Background()
	return client.SetNX(ctx, key, value, ttl).Result()
}

// ReleaseLock 释放一个分布式锁
func ReleaseLock(client *redis.Client, key string, value string) (bool, error) {
	ctx := context.Background()

	// 使用Lua脚本确保原子性操作
	script := `
	if redis.call("get", KEYS[1]) == ARGV[1] then
		return redis.call("del", KEYS[1])
	else
		return 0
	end`

	result, err := client.Eval(ctx, script, []string{key}, value).Result()
	if err != nil {
		return false, err
	}

	return result.(int64) == 1, nil
}

// StoreTimeSeries 存储时间序列数据
func StoreTimeSeries(client *redis.Client, key string, timestamp int64, value float64) error {
	ctx := context.Background()
	return client.HSet(ctx, key, timestamp, value).Err()
}

// GetTimeSeriesRange 获取时间范围内的时间序列数据
func GetTimeSeriesRange(client *redis.Client, key string, startTime, endTime int64) (map[string]string, error) {
	ctx := context.Background()
	return client.HGetAll(ctx, key).Result()
}

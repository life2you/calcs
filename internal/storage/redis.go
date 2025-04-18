package storage

import (
	"github.com/redis/go-redis/v9"
)

// RedisClient Redis客户端接口
type RedisClient interface {
	// Client 获取原始Redis客户端
	Client() *redis.Client

	// Close 关闭连接
	Close() error
}

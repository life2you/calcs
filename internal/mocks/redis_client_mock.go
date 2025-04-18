package mocks

import (
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/mock"
	"time"
)

// MockRedisClient Redis客户端的模拟实现
type MockRedisClient struct {
	mock.Mock
}

// Client 获取原始Redis客户端的模拟实现
func (m *MockRedisClient) Client() *redis.Client {
	args := m.Called()
	return args.Get(0).(*redis.Client)
}

// MockRedisClient Redis命令的模拟实现，用于测试
type MockRedisClientCommand struct {
	mock.Mock
}

// BRPop 阻塞式移除并获取列表最后一个元素的模拟实现
func (m *MockRedisClientCommand) BRPop(timeout time.Duration, keys ...string) *redis.StringSliceCmd {
	args := m.Called(timeout, keys)
	return args.Get(0).(*redis.StringSliceCmd)
}

// Close 关闭连接的模拟实现
func (m *MockRedisClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

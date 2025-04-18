package mocks

import (
	"github.com/life2you_mini/calcs/internal/exchange"
	"github.com/stretchr/testify/mock"
)

// MockExchangeFactory 交易所工厂接口的模拟实现
type MockExchangeFactory struct {
	mock.Mock
}

// Get 获取交易所客户端的模拟实现
func (m *MockExchangeFactory) Get(name string) (exchange.Exchange, bool) {
	args := m.Called(name)
	return args.Get(0).(exchange.Exchange), args.Bool(1)
}

// List 获取所有支持的交易所的模拟实现
func (m *MockExchangeFactory) List() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

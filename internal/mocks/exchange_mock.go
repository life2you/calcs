package mocks

import (
	"context"
	"time"

	"github.com/stretchr/testify/mock"
)

// MockExchange 交易所接口的模拟实现
type MockExchange struct {
	mock.Mock
}

// GetPrice 获取价格的模拟实现
func (m *MockExchange) GetPrice(ctx context.Context, symbol string) (float64, error) {
	args := m.Called(ctx, symbol)
	return args.Get(0).(float64), args.Error(1)
}

// GetFundingRate 获取资金费率的模拟实现
func (m *MockExchange) GetFundingRate(ctx context.Context, symbol string) (float64, time.Time, error) {
	args := m.Called(ctx, symbol)
	return args.Get(0).(float64), args.Get(1).(time.Time), args.Error(2)
}

// GetBalance 获取账户余额的模拟实现
func (m *MockExchange) GetBalance(ctx context.Context, asset string) (float64, error) {
	args := m.Called(ctx, asset)
	return args.Get(0).(float64), args.Error(1)
}

// PlaceOrder 下单的模拟实现
func (m *MockExchange) PlaceOrder(ctx context.Context, symbol, side, orderType string, quantity, price float64) (string, error) {
	args := m.Called(ctx, symbol, side, orderType, quantity, price)
	return args.String(0), args.Error(1)
}

// GetOrderStatus 获取订单状态的模拟实现
func (m *MockExchange) GetOrderStatus(ctx context.Context, symbol, orderID string) (string, error) {
	args := m.Called(ctx, symbol, orderID)
	return args.String(0), args.Error(1)
}

// CancelOrder 取消订单的模拟实现
func (m *MockExchange) CancelOrder(ctx context.Context, symbol, orderID string) error {
	args := m.Called(ctx, symbol, orderID)
	return args.Error(0)
}

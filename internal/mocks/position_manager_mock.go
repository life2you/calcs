package mocks

import (
	"context"

	"github.com/life2you_mini/calcs/internal/trading"
	"github.com/stretchr/testify/mock"
)

// MockPositionManager 持仓管理器的模拟实现
type MockPositionManager struct {
	mock.Mock
}

// CreatePosition 创建持仓的模拟实现
func (m *MockPositionManager) CreatePosition(ctx context.Context, position *trading.Position) error {
	args := m.Called(ctx, position)
	return args.Error(0)
}

// GetPosition 获取持仓的模拟实现
func (m *MockPositionManager) GetPosition(ctx context.Context, positionID string) (*trading.Position, error) {
	args := m.Called(ctx, positionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*trading.Position), args.Error(1)
}

// SavePosition 保存持仓的模拟实现
func (m *MockPositionManager) SavePosition(ctx context.Context, position *trading.Position) error {
	args := m.Called(ctx, position)
	return args.Error(0)
}

// UpdatePosition 更新持仓的模拟实现
func (m *MockPositionManager) UpdatePosition(ctx context.Context, position *trading.Position) error {
	args := m.Called(ctx, position)
	return args.Error(0)
}

// ClosePosition 关闭持仓的模拟实现
func (m *MockPositionManager) ClosePosition(ctx context.Context, positionID string, closePrice float64, pnl float64) error {
	args := m.Called(ctx, positionID, closePrice, pnl)
	return args.Error(0)
}

// ListOpenPositions 获取所有未平仓持仓ID的模拟实现
func (m *MockPositionManager) ListOpenPositions(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

// ListAllPositions 获取所有持仓ID的模拟实现
func (m *MockPositionManager) ListAllPositions(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

// DeletePosition 删除持仓的模拟实现
func (m *MockPositionManager) DeletePosition(ctx context.Context, positionID string) error {
	args := m.Called(ctx, positionID)
	return args.Error(0)
}

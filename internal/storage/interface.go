package storage

import (
	"context"
	"time"

	"github.com/life2you_mini/fundingarb/internal/exchange"
	"github.com/life2you_mini/fundingarb/internal/model"
)

// 存储类型常量
const (
	StorageTypeRedis    = "redis"
	StorageTypePostgres = "postgres"
	StorageTypeInMemory = "memory"
)

// Storage 定义存储层接口，可以有多种实现（Redis、PostgreSQL等）
type Storage interface {
	// 基础操作
	Initialize(ctx context.Context) error
	Close(ctx context.Context) error
	Health(ctx context.Context) error

	// 资金费率数据操作
	StoreFundingRate(ctx context.Context, data *exchange.FundingRateData) error
	GetFundingRates(ctx context.Context, exchange, symbol string, limit int) ([]*exchange.FundingRateData, error)
	GetAllFundingRates(ctx context.Context, limit int) ([]*exchange.FundingRateData, error)
	GetAverageFundingRate(ctx context.Context, exchange, symbol string, period time.Duration) (float64, error)

	// 交易持仓操作
	StorePosition(ctx context.Context, position *model.Position) error
	GetPositions(ctx context.Context, status string) ([]*model.Position, error)
	GetPositionByID(ctx context.Context, positionID string) (*model.Position, error)
	UpdatePosition(ctx context.Context, position *model.Position) error

	// 交易记录操作
	StoreTradeRecord(ctx context.Context, trade *model.TradeRecord) error
	GetTradeRecords(ctx context.Context, positionID string) ([]*model.TradeRecord, error)

	// 账户资金变动操作
	StoreAccountTransaction(ctx context.Context, transaction *model.AccountTransaction) error
	GetAccountTransactions(ctx context.Context, start, end time.Time) ([]*model.AccountTransaction, error)

	// 系统日志操作
	StoreSystemLog(ctx context.Context, log *model.SystemLog) error
	GetSystemLogs(ctx context.Context, level string, limit int) ([]*model.SystemLog, error)

	// 风险监控数据操作
	StoreRiskMetrics(ctx context.Context, metrics *model.RiskMetrics) error
	GetLatestRiskMetrics(ctx context.Context, positionID string) (*model.RiskMetrics, error)
}

// StorageFactory 存储工厂，用于创建不同的存储实现
type StorageFactory struct {
	implementations map[string]Storage
}

// NewStorageFactory 创建存储工厂
func NewStorageFactory() *StorageFactory {
	return &StorageFactory{
		implementations: make(map[string]Storage),
	}
}

// Register 注册存储实现
func (f *StorageFactory) Register(name string, storage Storage) {
	f.implementations[name] = storage
}

// Get 获取存储实现
func (f *StorageFactory) Get(name string) Storage {
	return f.implementations[name]
}

// GetAll 获取所有存储实现
func (f *StorageFactory) GetAll() []Storage {
	var result []Storage
	for _, storage := range f.implementations {
		result = append(result, storage)
	}
	return result
}

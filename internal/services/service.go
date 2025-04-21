package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/life2you_mini/calcs/internal/config"
	"github.com/life2you_mini/calcs/internal/exchange"
	"github.com/life2you_mini/calcs/internal/monitor"
	_redisClient "github.com/life2you_mini/calcs/internal/redis"
	"github.com/life2you_mini/calcs/internal/trading"
)

// calcsService 资金费率套利服务
type calcsService struct {
	ctx             context.Context
	cancel          context.CancelFunc
	logger          *zap.Logger
	exchangeFactory *exchange.ExchangeFactory
	redisClient     *_redisClient.StorageClient
	fundingMonitor  *monitor.FundingRateMonitor
	trader          *trading.Trader
}

// NewcalcsService 创建新的资金费率套利服务
func NewcalcsService(
	parentCtx context.Context,
	cfg *config.Config,
	logger *zap.Logger,
) (*calcsService, error) {
	// 创建服务上下文
	ctx, cancel := context.WithCancel(parentCtx)

	// 初始化Redis客户端封装
	redisAddr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	redisClient, err := _redisClient.NewStorageClient(
		redisAddr,
		cfg.Redis.Password,
		cfg.Redis.DB,
		cfg.Redis.KeyPrefix,
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("初始化Redis客户端失败: %w", err)
	}

	// 创建交易所工厂
	exchangeFactory := exchange.CreateExchangeFactory(
		logger,
		&exchange.BinanceConfig{
			Enabled:   cfg.Exchanges.Binance.Enabled,
			APIKey:    cfg.Exchanges.Binance.APIKey,
			APISecret: cfg.Exchanges.Binance.APISecret,
		},
		&exchange.OKXConfig{
			Enabled:    cfg.Exchanges.OKX.Enabled,
			APIKey:     cfg.Exchanges.OKX.APIKey,
			APISecret:  cfg.Exchanges.OKX.APISecret,
			Passphrase: cfg.Exchanges.OKX.Passphrase,
		},
		&exchange.BitgetConfig{
			Enabled:    cfg.Exchanges.Bitget.Enabled,
			APIKey:     cfg.Exchanges.Bitget.APIKey,
			APISecret:  cfg.Exchanges.Bitget.APISecret,
			Passphrase: cfg.Exchanges.Bitget.Passphrase,
		},
	)

	// 获取所有已注册的交易所
	exchanges := make([]monitor.ExchangeAPI, 0)
	for _, exchange := range exchangeFactory.GetAll() {
		// 使用适配器将 exchange.Exchange 转换为 monitor.ExchangeAPI
		adapter := NewExchangeAdapter(exchange)
		exchanges = append(exchanges, adapter)
	}

	// 创建资金费率监控器
	fundingMonitor := monitor.NewFundingRateMonitor(
		exchanges,
		logger,
		cfg.Trading.MinYearlyFundingRate,
		cfg.Trading.AllowedSymbols,
		NewRedisStorageAdapter(redisClient),
	)

	// 设置检查间隔
	checkInterval := time.Duration(cfg.System.FundingRateCheckIntervalMinutes) * time.Minute
	fundingMonitor.SetCheckInterval(checkInterval)

	// 创建交易执行器
	trader := trading.NewTrader(
		ctx,
		cfg,
		logger.With(zap.String("component", "trader")),
		redisClient,
		exchangeFactory,
	)

	return &calcsService{
		ctx:             ctx,
		cancel:          cancel,
		logger:          logger,
		exchangeFactory: exchangeFactory,
		redisClient:     redisClient,
		fundingMonitor:  fundingMonitor,
		trader:          trader,
	}, nil
}

// Start 启动服务
func (s *calcsService) Start() {
	s.logger.Info("启动资金费率套利服务")

	// 启动资金费率监控
	go func() {
		if err := s.fundingMonitor.Start(s.ctx); err != nil {
			s.logger.Error("资金费率监控启动失败", zap.Error(err))
		}
	}()

	// 启动交易执行器
	go func() {
		if err := s.trader.Start(); err != nil {
			s.logger.Error("交易执行器启动失败", zap.Error(err))
		}
	}()
}

// Stop 停止服务
func (s *calcsService) Stop(ctx context.Context) error {
	s.logger.Info("停止资金费率套利服务")

	// 停止交易执行器
	if err := s.trader.Stop(); err != nil {
		s.logger.Error("停止交易执行器失败", zap.Error(err))
	}

	// 取消服务上下文
	s.cancel()

	// 关闭Redis连接
	if err := s.redisClient.Close(); err != nil {
		s.logger.Error("关闭Redis连接失败", zap.Error(err))
	}

	// 等待服务优雅关闭的超时时间
	shutdownTimeout := 5 * time.Second

	// 创建定时器
	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()

	// 等待服务关闭或超时
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

package exchange

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ccxt/ccxt/go/v4/go"
	"go.uber.org/zap"
)

// BinanceClient 币安交易所客户端
type BinanceClient struct {
	exchange *ccxt.Binance
	logger   *zap.Logger
}

// NewBinanceClient 创建新的币安客户端
func NewBinanceClient(apiKey, apiSecret string, logger *zap.Logger) *BinanceClient {
	// 创建CCXT的Binance实例
	binanceInstance := ccxt.NewBinance(map[string]interface{}{
		"apiKey":          apiKey,
		"secret":          apiSecret,
		"enableRateLimit": true,
	})

	// 加载市场信息
	go func() {
		<-binanceInstance.LoadMarkets()
		logger.Info("Binance市场数据加载完成")
	}()

	return &BinanceClient{
		exchange: binanceInstance,
		logger:   logger,
	}
}

// GetExchangeName 获取交易所名称
func (b *BinanceClient) GetExchangeName() string {
	return "Binance"
}

// GetFundingRate 获取指定交易对的资金费率
func (b *BinanceClient) GetFundingRate(ctx context.Context, symbol string) (*FundingRateData, error) {
	// CCXT格式化交易对名称
	formattedSymbol := formatBinanceSymbol(symbol)

	// 使用CCXT获取资金费率
	fundingRateData, err := b.exchange.FetchFundingRate(formattedSymbol)
	if err != nil {
		b.logger.Error("获取币安资金费率失败",
			zap.Error(err),
			zap.String("symbol", symbol))
		return nil, fmt.Errorf("获取币安资金费率失败: %w", err)
	}

	// 解析数据
	fundingRate, err := strconv.ParseFloat(fmt.Sprintf("%v", (*fundingRateData)["fundingRate"]), 64)
	if err != nil {
		return nil, fmt.Errorf("解析资金费率失败: %w", err)
	}

	// 获取下次资金费率时间
	nextFundingTimeMs, ok := (*fundingRateData)["nextFundingTime"].(int64)
	if !ok {
		b.logger.Warn("无法获取下次资金费率时间",
			zap.String("symbol", symbol))
		nextFundingTimeMs = time.Now().Add(8 * time.Hour).UnixMilli()
	}

	// 转换为我们的数据结构
	result := &FundingRateData{
		Exchange:        b.GetExchangeName(),
		Symbol:          symbol,
		FundingRate:     fundingRate,
		YearlyRate:      CalculateYearlyRate(fundingRate),
		NextFundingTime: time.UnixMilli(nextFundingTimeMs),
		Timestamp:       time.Now(),
	}

	return result, nil
}

// GetAllFundingRates 获取所有交易对的资金费率
func (b *BinanceClient) GetAllFundingRates(ctx context.Context) ([]*FundingRateData, error) {
	// 获取所有资金费率
	fundingRates, err := b.exchange.FetchFundingRates(nil)
	if err != nil {
		b.logger.Error("获取币安所有资金费率失败", zap.Error(err))
		return nil, fmt.Errorf("获取币安所有资金费率失败: %w", err)
	}

	result := make([]*FundingRateData, 0, len(*fundingRates))
	now := time.Now()

	// 遍历处理每个交易对
	for symbol, rateData := range *fundingRates {
		// CCXT格式的交易对转换回我们的格式
		standardSymbol := formatStandardBinanceSymbol(symbol)

		rateObj, ok := rateData.(map[string]interface{})
		if !ok {
			b.logger.Warn("资金费率数据格式错误",
				zap.String("symbol", symbol))
			continue
		}

		// 解析资金费率
		fundingRate, err := strconv.ParseFloat(fmt.Sprintf("%v", rateObj["fundingRate"]), 64)
		if err != nil {
			b.logger.Warn("解析资金费率失败",
				zap.String("symbol", symbol),
				zap.Error(err))
			continue
		}

		// 获取下次资金费率时间
		nextFundingTimeMs, ok := rateObj["nextFundingTime"].(int64)
		if !ok {
			nextFundingTimeMs = now.Add(8 * time.Hour).UnixMilli()
		}

		data := &FundingRateData{
			Exchange:        b.GetExchangeName(),
			Symbol:          standardSymbol,
			FundingRate:     fundingRate,
			YearlyRate:      CalculateYearlyRate(fundingRate),
			NextFundingTime: time.UnixMilli(nextFundingTimeMs),
			Timestamp:       now,
		}

		result = append(result, data)
	}

	return result, nil
}

// GetMarketDepth 获取市场深度数据
func (b *BinanceClient) GetMarketDepth(ctx context.Context, symbol string) (map[string]interface{}, error) {
	// 合约交易对格式
	futureSymbol := formatBinanceSymbol(symbol)

	// 获取合约市场深度
	contractOrderBookParams := map[string]interface{}{
		"limit": 10,
	}
	contractOrderBook, err := b.exchange.FetchOrderBook(futureSymbol, contractOrderBookParams)
	if err != nil {
		b.logger.Error("获取币安合约深度失败",
			zap.Error(err),
			zap.String("symbol", symbol))
		return nil, fmt.Errorf("获取币安合约深度失败: %w", err)
	}

	// 现货交易对格式
	spotSymbol := formatBinanceSpotSymbol(symbol)

	// 获取现货市场深度
	spotOrderBookParams := map[string]interface{}{
		"limit": 10,
	}
	spotOrderBook, err := b.exchange.FetchOrderBook(spotSymbol, spotOrderBookParams)
	if err != nil {
		b.logger.Error("获取币安现货深度失败",
			zap.Error(err),
			zap.String("symbol", symbol))
		return nil, fmt.Errorf("获取币安现货深度失败: %w", err)
	}

	result := map[string]interface{}{
		"symbol":    symbol,
		"future":    contractOrderBook,
		"spot":      spotOrderBook,
		"timestamp": time.Now().Unix(),
	}

	return result, nil
}

// GetPrice 获取最新价格
func (b *BinanceClient) GetPrice(ctx context.Context, symbol string) (float64, error) {
	formattedSymbol := formatBinanceSymbol(symbol)

	ticker, err := b.exchange.FetchTicker(formattedSymbol)
	if err != nil {
		b.logger.Error("获取币安价格失败",
			zap.Error(err),
			zap.String("symbol", symbol))
		return 0, fmt.Errorf("获取币安价格失败: %w", err)
	}

	lastPrice, ok := (*ticker)["last"].(float64)
	if !ok {
		return 0, fmt.Errorf("价格数据格式错误")
	}

	return lastPrice, nil
}

// SetLeverage 设置杠杆倍数
func (b *BinanceClient) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	formattedSymbol := formatBinanceSymbol(symbol)

	// 设置杠杆
	params := map[string]interface{}{
		"leverage": leverage,
	}

	_, err := b.exchange.SetLeverage(leverage, formattedSymbol, params)
	if err != nil {
		b.logger.Error("设置币安杠杆失败",
			zap.Error(err),
			zap.String("symbol", symbol),
			zap.Int("leverage", leverage))
		return fmt.Errorf("设置币安杠杆失败: %w", err)
	}

	b.logger.Info("成功设置杠杆",
		zap.String("symbol", symbol),
		zap.Int("leverage", leverage))
	return nil
}

// CreateContractOrder 创建合约订单
func (b *BinanceClient) CreateContractOrder(ctx context.Context, symbol string, side string, positionSide string, orderType string, quantity float64, price float64) (string, error) {
	formattedSymbol := formatBinanceSymbol(symbol)

	// 转换为CCXT格式的交易类型
	var ccxtType string
	if orderType == "MARKET" {
		ccxtType = "market"
	} else {
		ccxtType = "limit"
	}

	// 转换为CCXT格式的交易方向
	var ccxtSide string
	if side == "BUY" {
		ccxtSide = "buy"
	} else {
		ccxtSide = "sell"
	}

	// 准备参数
	params := map[string]interface{}{
		"marginMode": "cross", // 全仓模式
	}

	// 添加持仓方向参数
	if positionSide == "LONG" {
		params["positionSide"] = "LONG"
	} else if positionSide == "SHORT" {
		params["positionSide"] = "SHORT"
	}

	// 创建订单
	var order *map[string]interface{}
	var err error
	if ccxtType == "limit" {
		options := []func(*ccxt.CreateOrderOpts){
			ccxt.WithCreateOrderSymbol(formattedSymbol),
			ccxt.WithCreateOrderType(ccxtType),
			ccxt.WithCreateOrderSide(ccxtSide),
			ccxt.WithCreateOrderAmount(quantity),
			ccxt.WithCreateOrderPrice(price),
			ccxt.WithCreateOrderParams(params),
		}
		order, err = b.exchange.CreateOrder(options...)
	} else {
		options := []func(*ccxt.CreateOrderOpts){
			ccxt.WithCreateOrderSymbol(formattedSymbol),
			ccxt.WithCreateOrderType(ccxtType),
			ccxt.WithCreateOrderSide(ccxtSide),
			ccxt.WithCreateOrderAmount(quantity),
			ccxt.WithCreateOrderParams(params),
		}
		order, err = b.exchange.CreateOrder(options...)
	}

	if err != nil {
		b.logger.Error("创建币安合约订单失败",
			zap.Error(err),
			zap.String("symbol", symbol),
			zap.String("side", side),
			zap.String("positionSide", positionSide),
			zap.Float64("quantity", quantity))
		return "", fmt.Errorf("创建币安合约订单失败: %w", err)
	}

	// 提取订单ID
	orderId, ok := (*order)["id"].(string)
	if !ok {
		return "", fmt.Errorf("订单ID不存在或格式错误")
	}

	b.logger.Info("成功创建合约订单",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.String("positionSide", positionSide),
		zap.String("orderId", orderId))

	return orderId, nil
}

// CreateSpotOrder 创建现货订单
func (b *BinanceClient) CreateSpotOrder(ctx context.Context, symbol string, side string, orderType string, quantity float64, price float64) (string, error) {
	// 转换为现货交易对格式
	formattedSymbol := formatBinanceSpotSymbol(symbol)

	// 转换为CCXT格式的交易类型
	var ccxtType string
	if orderType == "MARKET" {
		ccxtType = "market"
	} else {
		ccxtType = "limit"
	}

	// 转换为CCXT格式的交易方向
	var ccxtSide string
	if side == "BUY" {
		ccxtSide = "buy"
	} else {
		ccxtSide = "sell"
	}

	// 创建订单
	var order *map[string]interface{}
	var err error
	if ccxtType == "limit" {
		options := []func(*ccxt.CreateOrderOpts){
			ccxt.WithCreateOrderSymbol(formattedSymbol),
			ccxt.WithCreateOrderType(ccxtType),
			ccxt.WithCreateOrderSide(ccxtSide),
			ccxt.WithCreateOrderAmount(quantity),
			ccxt.WithCreateOrderPrice(price),
		}
		order, err = b.exchange.CreateOrder(options...)
	} else {
		options := []func(*ccxt.CreateOrderOpts){
			ccxt.WithCreateOrderSymbol(formattedSymbol),
			ccxt.WithCreateOrderType(ccxtType),
			ccxt.WithCreateOrderSide(ccxtSide),
			ccxt.WithCreateOrderAmount(quantity),
		}
		order, err = b.exchange.CreateOrder(options...)
	}

	if err != nil {
		b.logger.Error("创建币安现货订单失败",
			zap.Error(err),
			zap.String("symbol", symbol),
			zap.String("side", side),
			zap.Float64("quantity", quantity))
		return "", fmt.Errorf("创建币安现货订单失败: %w", err)
	}

	// 提取订单ID
	orderId, ok := (*order)["id"].(string)
	if !ok {
		return "", fmt.Errorf("订单ID不存在或格式错误")
	}

	b.logger.Info("成功创建现货订单",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.String("orderId", orderId))

	return orderId, nil
}

// 辅助函数：将BTC/USDT格式的交易对转换为Binance合约使用的格式
func formatBinanceSymbol(symbol string) string {
	// 币安合约通常使用BTCUSDT格式（不带斜杠）
	return strings.ReplaceAll(symbol, "/", "")
}

// 辅助函数：将BTC/USDT格式的交易对转换为Binance现货格式
func formatBinanceSpotSymbol(symbol string) string {
	// 币安现货和合约使用相同格式
	return strings.ReplaceAll(symbol, "/", "")
}

// 辅助函数：将Binance格式的交易对转换回标准格式
func formatStandardBinanceSymbol(symbol string) string {
	// 为BTCUSDT格式的交易对添加斜杠
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		return fmt.Sprintf("%s/USDT", base)
	}

	return symbol
}

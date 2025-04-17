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

// OKXClient 使用CCXT实现的OKX交易所客户端
type OKXClient struct {
	exchange *ccxt.OKX
	logger   *zap.Logger
}

// NewOKXClient 创建新的OKX客户端
func NewOKXClient(apiKey, apiSecret, passphrase string, logger *zap.Logger) *OKXClient {
	// 创建CCXT的OKX实例
	okxInstance := ccxt.NewOKX(map[string]interface{}{
		"apiKey":          apiKey,
		"secret":          apiSecret,
		"password":        passphrase,
		"enableRateLimit": true,
	})

	// 加载市场信息
	go func() {
		<-okxInstance.LoadMarkets()
		logger.Info("OKX市场数据加载完成")
	}()

	return &OKXClient{
		exchange: okxInstance,
		logger:   logger,
	}
}

// GetExchangeName 获取交易所名称
func (o *OKXClient) GetExchangeName() string {
	return "OKX"
}

// GetFundingRate 获取指定交易对的资金费率
func (o *OKXClient) GetFundingRate(ctx context.Context, symbol string) (*FundingRateData, error) {
	// CCXT格式化交易对名称
	formattedSymbol := formatCCXTSymbol(symbol)

	// 使用CCXT获取资金费率
	fundingRateData, err := o.exchange.FetchFundingRate(formattedSymbol)
	if err != nil {
		o.logger.Error("获取OKX资金费率失败",
			zap.Error(err),
			zap.String("symbol", symbol))
		return nil, fmt.Errorf("获取OKX资金费率失败: %w", err)
	}

	// 解析数据
	fundingRate, err := strconv.ParseFloat(fmt.Sprintf("%v", (*fundingRateData)["fundingRate"]), 64)
	if err != nil {
		return nil, fmt.Errorf("解析资金费率失败: %w", err)
	}

	// 获取下次资金费率时间
	nextFundingTimeMs, ok := (*fundingRateData)["nextFundingTime"].(int64)
	if !ok {
		o.logger.Warn("无法获取下次资金费率时间",
			zap.String("symbol", symbol))
		nextFundingTimeMs = time.Now().Add(8 * time.Hour).UnixMilli()
	}

	// 转换为我们的数据结构
	result := &FundingRateData{
		Exchange:        o.GetExchangeName(),
		Symbol:          symbol,
		FundingRate:     fundingRate,
		YearlyRate:      CalculateYearlyRate(fundingRate),
		NextFundingTime: time.UnixMilli(nextFundingTimeMs),
		Timestamp:       time.Now(),
	}

	return result, nil
}

// GetAllFundingRates 获取所有交易对的资金费率
func (o *OKXClient) GetAllFundingRates(ctx context.Context) ([]*FundingRateData, error) {
	// 获取所有资金费率
	fundingRates, err := o.exchange.FetchFundingRates(nil)
	if err != nil {
		o.logger.Error("获取OKX所有资金费率失败", zap.Error(err))
		return nil, fmt.Errorf("获取OKX所有资金费率失败: %w", err)
	}

	result := make([]*FundingRateData, 0, len(*fundingRates))
	now := time.Now()

	// 遍历处理每个交易对
	for symbol, rateData := range *fundingRates {
		// CCXT格式的交易对转换回我们的格式
		standardSymbol := formatStandardSymbol(symbol)

		rateObj, ok := rateData.(map[string]interface{})
		if !ok {
			o.logger.Warn("资金费率数据格式错误",
				zap.String("symbol", symbol))
			continue
		}

		// 解析资金费率
		fundingRate, err := strconv.ParseFloat(fmt.Sprintf("%v", rateObj["fundingRate"]), 64)
		if err != nil {
			o.logger.Warn("解析资金费率失败",
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
			Exchange:        o.GetExchangeName(),
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
func (o *OKXClient) GetMarketDepth(ctx context.Context, symbol string) (map[string]interface{}, error) {
	formattedSymbol := formatCCXTSymbol(symbol)

	// 获取合约市场深度
	contractOrderBookParams := map[string]interface{}{
		"limit": 10,
	}
	contractOrderBook, err := o.exchange.FetchOrderBook(formattedSymbol, contractOrderBookParams)
	if err != nil {
		o.logger.Error("获取OKX合约深度失败",
			zap.Error(err),
			zap.String("symbol", symbol))
		return nil, fmt.Errorf("获取OKX合约深度失败: %w", err)
	}

	// 获取现货市场深度 (需要处理现货交易对格式)
	spotSymbol := strings.Replace(formattedSymbol, "SWAP", "SPOT", 1)
	spotOrderBookParams := map[string]interface{}{
		"limit": 10,
	}
	spotOrderBook, err := o.exchange.FetchOrderBook(spotSymbol, spotOrderBookParams)
	if err != nil {
		o.logger.Error("获取OKX现货深度失败",
			zap.Error(err),
			zap.String("symbol", symbol))
		return nil, fmt.Errorf("获取OKX现货深度失败: %w", err)
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
func (o *OKXClient) GetPrice(ctx context.Context, symbol string) (float64, error) {
	formattedSymbol := formatCCXTSymbol(symbol)

	ticker, err := o.exchange.FetchTicker(formattedSymbol)
	if err != nil {
		o.logger.Error("获取OKX价格失败",
			zap.Error(err),
			zap.String("symbol", symbol))
		return 0, fmt.Errorf("获取OKX价格失败: %w", err)
	}

	lastPrice, ok := (*ticker)["last"].(float64)
	if !ok {
		return 0, fmt.Errorf("价格数据格式错误")
	}

	return lastPrice, nil
}

// SetLeverage 设置杠杆倍数
func (o *OKXClient) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	formattedSymbol := formatCCXTSymbol(symbol)

	// 设置杠杆
	params := map[string]interface{}{
		"leverage": leverage,
	}

	_, err := o.exchange.SetLeverage(leverage, formattedSymbol, params)
	if err != nil {
		o.logger.Error("设置OKX杠杆失败",
			zap.Error(err),
			zap.String("symbol", symbol),
			zap.Int("leverage", leverage))
		return fmt.Errorf("设置OKX杠杆失败: %w", err)
	}

	o.logger.Info("成功设置杠杆",
		zap.String("symbol", symbol),
		zap.Int("leverage", leverage))
	return nil
}

// CreateContractOrder 创建合约订单
func (o *OKXClient) CreateContractOrder(ctx context.Context, symbol string, side string, positionSide string, orderType string, quantity float64, price float64) (string, error) {
	formattedSymbol := formatCCXTSymbol(symbol)

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
		"tdMode": "cross", // 全仓模式
	}

	// 根据持仓方向添加参数
	if positionSide == "LONG" {
		params["posSide"] = "long"
	} else if positionSide == "SHORT" {
		params["posSide"] = "short"
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
		order, err = o.exchange.CreateOrder(options...)
	} else {
		options := []func(*ccxt.CreateOrderOpts){
			ccxt.WithCreateOrderSymbol(formattedSymbol),
			ccxt.WithCreateOrderType(ccxtType),
			ccxt.WithCreateOrderSide(ccxtSide),
			ccxt.WithCreateOrderAmount(quantity),
			ccxt.WithCreateOrderParams(params),
		}
		order, err = o.exchange.CreateOrder(options...)
	}

	if err != nil {
		o.logger.Error("创建OKX合约订单失败",
			zap.Error(err),
			zap.String("symbol", symbol),
			zap.String("side", side),
			zap.String("positionSide", positionSide),
			zap.Float64("quantity", quantity))
		return "", fmt.Errorf("创建OKX合约订单失败: %w", err)
	}

	// 提取订单ID
	orderId, ok := (*order)["id"].(string)
	if !ok {
		return "", fmt.Errorf("订单ID不存在或格式错误")
	}

	o.logger.Info("成功创建合约订单",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.String("positionSide", positionSide),
		zap.String("orderId", orderId))

	return orderId, nil
}

// CreateSpotOrder 创建现货订单
func (o *OKXClient) CreateSpotOrder(ctx context.Context, symbol string, side string, orderType string, quantity float64, price float64) (string, error) {
	// 转换为现货交易对格式
	formattedSymbol := formatCCXTSpotSymbol(symbol)

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
		order, err = o.exchange.CreateOrder(options...)
	} else {
		options := []func(*ccxt.CreateOrderOpts){
			ccxt.WithCreateOrderSymbol(formattedSymbol),
			ccxt.WithCreateOrderType(ccxtType),
			ccxt.WithCreateOrderSide(ccxtSide),
			ccxt.WithCreateOrderAmount(quantity),
		}
		order, err = o.exchange.CreateOrder(options...)
	}

	if err != nil {
		o.logger.Error("创建OKX现货订单失败",
			zap.Error(err),
			zap.String("symbol", symbol),
			zap.String("side", side),
			zap.Float64("quantity", quantity))
		return "", fmt.Errorf("创建OKX现货订单失败: %w", err)
	}

	// 提取订单ID
	orderId, ok := (*order)["id"].(string)
	if !ok {
		return "", fmt.Errorf("订单ID不存在或格式错误")
	}

	o.logger.Info("成功创建现货订单",
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.String("orderId", orderId))

	return orderId, nil
}

// 辅助函数：将BTC/USDT格式的交易对转换为CCXT使用的格式
func formatCCXTSymbol(symbol string) string {
	// OKX合约通常使用BTC-USDT-SWAP格式
	parts := strings.Split(symbol, "/")
	if len(parts) == 2 {
		return fmt.Sprintf("%s-%s-SWAP", parts[0], parts[1])
	}
	return symbol
}

// 辅助函数：将BTC/USDT格式的交易对转换为CCXT现货格式
func formatCCXTSpotSymbol(symbol string) string {
	// OKX现货通常使用BTC-USDT格式
	parts := strings.Split(symbol, "/")
	if len(parts) == 2 {
		return fmt.Sprintf("%s-%s", parts[0], parts[1])
	}
	return symbol
}

// 辅助函数：将CCXT格式的交易对转换回标准格式
func formatStandardSymbol(symbol string) string {
	// 转换BTC-USDT-SWAP或BTC-USDT到BTC/USDT
	symbol = strings.Replace(symbol, "-SWAP", "", 1)
	parts := strings.Split(symbol, "-")
	if len(parts) >= 2 {
		return fmt.Sprintf("%s/%s", parts[0], parts[1])
	}
	return symbol
}

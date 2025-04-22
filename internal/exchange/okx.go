package exchange

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/life2you_mini/calcs/internal/config"

	ccxt "github.com/ccxt/ccxt/go/v4"
	"go.uber.org/zap"
)

// OKXClient OKX交易所客户端
type OKXClient struct {
	exchange ccxt.Okx
	logger   *zap.Logger
}

// NewOKXClient 创建新的OKX客户端
func NewOKXClient(apiKey, apiSecret, password string, logger *zap.Logger, proxy *config.HttpProxyConfig) *OKXClient {
	// 创建配置对象
	clientConfig := map[string]interface{}{
		"apiKey":          apiKey,
		"secret":          apiSecret,
		"password":        password,
		"enableRateLimit": true,
		"timeout":         30000, // 添加超时配置
	}

	// 增加日志：打印传入的代理配置
	if proxy != nil {
		logger.Info("传入的代理配置",
			zap.Bool("enabled", proxy.Enabled),
			zap.String("httpProxy", proxy.HttpProxy),
			zap.String("httpsProxy", proxy.HttpsProxy),
		)
	} else {
		logger.Warn("传入的代理配置为 nil")
	}

	// 如果设置了代理，记录日志，但不再通过代码配置代理给CCXT
	if proxy != nil && proxy.Enabled && (proxy.HttpsProxy != "" || proxy.HttpProxy != "") {
		proxyToUse := proxy.HttpsProxy
		if proxyToUse == "" {
			proxyToUse = proxy.HttpProxy
		}
		logger.Info("代码配置：计划使用HTTP代理（将通过环境变量生效）", zap.String("proxy", proxyToUse))
		// 移除：clientConfig["proxy"] = proxyToUse
	} else {
		logger.Info("代码配置：未使用代理或配置无效")
	}

	// 创建CCXT的OKX实例
	okxInstance := ccxt.NewOkx(clientConfig)

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
	formattedSymbol := formatOKXSymbol(symbol)
	fundingRateData, err := o.exchange.FetchFundingRate(formattedSymbol)
	if err != nil {
		o.logger.Error("获取OKX资金费率失败", zap.Error(err), zap.String("symbol", symbol))
		return nil, fmt.Errorf("获取OKX资金费率失败: %w", err)
	}

	if fundingRateData.FundingRate == nil {
		o.logger.Warn("OKX资金费率值为空", zap.String("symbol", symbol))
		return nil, fmt.Errorf("资金费率值为空 for %s", symbol)
	}
	fundingRate := *fundingRateData.FundingRate

	var nextFundingTimeMs int64
	if fundingRateData.NextFundingTimestamp != nil {
		nextFundingTimeMs = int64(*fundingRateData.NextFundingTimestamp)
	} else {
		o.logger.Warn("NextFundingTimestamp 为 nil，使用默认8小时", zap.String("symbol", symbol))
		nextFundingTimeMs = time.Now().Add(8 * time.Hour).UnixMilli()
	}

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
	ratesChan := o.exchange.FetchFundingRates(ccxt.WithFetchFundingRatesParams(map[string]interface{}{}))

	timeout := time.After(30 * time.Second)

	select {
	case result := <-ratesChan:
		if err, ok := result.(error); ok {
			o.logger.Error("获取OKX所有资金费率失败 (channel error)", zap.Error(err))
			return nil, fmt.Errorf("获取OKX所有资金费率失败: %w", err)
		}

		if ratesResponse, ok := result.(*ccxt.FundingRates); ok {
			if ratesResponse == nil || ratesResponse.FundingRates == nil {
				o.logger.Warn("FetchFundingRates channel 返回了 nil 数据或空的费率 map")
				return []*FundingRateData{}, nil
			}

			var fundingRatesResult []*FundingRateData
			now := time.Now()
			for symbol, rate := range ratesResponse.FundingRates {
				if rate.FundingRate == nil {
					continue
				}
				standardSymbol := formatStandardOKXSymbol(symbol)
				fundingRate := *rate.FundingRate
				var nextFundingTimeMs int64
				if rate.NextFundingTimestamp != nil {
					nextFundingTimeMs = int64(*rate.NextFundingTimestamp)
				} else {
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
				fundingRatesResult = append(fundingRatesResult, data)
			}
			return fundingRatesResult, nil

		} else {
			o.logger.Error("获取OKX所有资金费率失败: channel 返回了未知类型", zap.Any("type", fmt.Sprintf("%T", result)))
			return nil, fmt.Errorf("获取OKX所有资金费率失败: 返回了未知类型 %T", result)
		}

	case <-ctx.Done():
		o.logger.Warn("获取OKX所有资金费率被取消", zap.Error(ctx.Err()))
		return nil, fmt.Errorf("获取OKX所有资金费率被取消: %w", ctx.Err())
	case <-timeout:
		o.logger.Error("获取OKX所有资金费率超时")
		return nil, fmt.Errorf("获取OKX所有资金费率超时")
	}
}

// GetMarketDepth 获取市场深度数据
func (o *OKXClient) GetMarketDepth(ctx context.Context, symbol string) (map[string]interface{}, error) {
	futureSymbol := formatOKXSymbol(symbol)
	contractOrderBook, err := o.exchange.FetchOrderBook(futureSymbol, ccxt.WithFetchOrderBookLimit(10))
	if err != nil {
		o.logger.Error("获取OKX合约深度失败", zap.Error(err), zap.String("symbol", symbol))
	}

	spotSymbol := formatOKXSpotSymbol(symbol)
	spotOrderBook, spotErr := o.exchange.FetchOrderBook(spotSymbol, ccxt.WithFetchOrderBookLimit(10))
	if spotErr != nil {
		o.logger.Error("获取OKX现货深度失败", zap.Error(spotErr), zap.String("symbol", symbol))
		if err != nil {
			return nil, fmt.Errorf("获取OKX合约和现货深度均失败: contract_err=%w, spot_err=%w", err, spotErr)
		}
	}

	contractHasData := contractOrderBook.Bids != nil && len(contractOrderBook.Bids) > 0 || contractOrderBook.Asks != nil && len(contractOrderBook.Asks) > 0
	spotHasData := spotOrderBook.Bids != nil && len(spotOrderBook.Bids) > 0 || spotOrderBook.Asks != nil && len(spotOrderBook.Asks) > 0

	if !contractHasData && !spotHasData {
		finalErr := spotErr
		if finalErr == nil {
			finalErr = err
		}
		if finalErr == nil {
			finalErr = fmt.Errorf("无法获取 %s 的合约或现货市场深度数据", symbol)
		}
		return nil, fmt.Errorf("获取OKX市场深度失败: %w", finalErr)
	}

	result := map[string]interface{}{
		"symbol":    symbol,
		"timestamp": time.Now().UnixMilli(),
	}
	if contractHasData {
		result["future"] = contractOrderBook
	}
	if spotHasData {
		result["spot"] = spotOrderBook
	}

	return result, nil
}

// GetPrice 获取最新价格
func (o *OKXClient) GetPrice(ctx context.Context, symbol string) (float64, error) {
	formattedSymbol := formatOKXSymbol(symbol)
	ticker, err := o.exchange.FetchTicker(formattedSymbol)
	if err != nil {
		o.logger.Error("获取OKX价格失败", zap.Error(err), zap.String("symbol", symbol))
		return 0, fmt.Errorf("获取OKX价格失败: %w", err)
	}
	if ticker.Last == nil {
		o.logger.Warn("OKX价格数据为空", zap.String("symbol", symbol))
		return 0, fmt.Errorf("价格数据为空 for %s", symbol)
	}
	return *ticker.Last, nil
}

// SetLeverage 设置杠杆倍数
func (o *OKXClient) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	params := map[string]interface{}{}
	_, err := o.exchange.SetLeverage(int64(leverage), ccxt.WithSetLeverageParams(params))
	if err != nil {
		o.logger.Error("设置OKX杠杆失败", zap.Error(err), zap.String("symbol", symbol), zap.Int("leverage", leverage))
		return fmt.Errorf("设置OKX杠杆失败: %w", err)
	}
	o.logger.Info("成功设置杠杆", zap.String("symbol", symbol), zap.Int("leverage", leverage))
	return nil
}

// CreateContractOrder 创建合约订单
func (o *OKXClient) CreateContractOrder(ctx context.Context, symbol string, side string, positionSide string, orderType string, quantity float64, price float64) (string, error) {
	formattedSymbol := formatCCXTSymbol(symbol)
	var ccxtType string
	var priceOpt ccxt.CreateOrderOptions
	if strings.ToUpper(orderType) == "MARKET" {
		ccxtType = "market"
	} else {
		ccxtType = "limit"
		if price <= 0 {
			return "", fmt.Errorf("limit order requires a positive price")
		}
		priceOpt = ccxt.WithCreateOrderPrice(price)
	}
	var ccxtSide string
	if strings.ToUpper(side) == "BUY" {
		ccxtSide = "buy"
	} else if strings.ToUpper(side) == "SELL" {
		ccxtSide = "sell"
	} else {
		return "", fmt.Errorf("invalid side: %s", side)
	}

	params := map[string]interface{}{"tdMode": "cross"}
	if strings.ToUpper(positionSide) == "LONG" {
		params["posSide"] = "long"
	} else if strings.ToUpper(positionSide) == "SHORT" {
		params["posSide"] = "short"
	} else {
		return "", fmt.Errorf("invalid positionSide: %s", positionSide)
	}
	paramsOpt := ccxt.WithCreateOrderParams(params)

	var opts []ccxt.CreateOrderOptions
	if priceOpt != nil {
		opts = append(opts, priceOpt)
	}
	opts = append(opts, paramsOpt)

	orderResult, err := o.exchange.CreateOrder(formattedSymbol, ccxtType, ccxtSide, quantity, opts...)
	if err != nil {
		o.logger.Error("创建OKX合约订单失败", zap.Error(err), zap.String("symbol", symbol), zap.String("side", side), zap.String("positionSide", positionSide), zap.Float64("quantity", quantity), zap.Float64("price", price))
		return "", fmt.Errorf("创建OKX合约订单失败: %w", err)
	}

	if orderResult.Id == nil || *orderResult.Id == "" {
		o.logger.Error("无法从OKX订单结果中提取订单ID", zap.Any("orderResult", orderResult))
		return "", fmt.Errorf("无法从OKX订单结果中提取订单ID")
	}
	orderID := *orderResult.Id

	o.logger.Info("成功创建OKX合约订单", zap.String("orderID", orderID), zap.String("symbol", symbol), zap.String("side", side), zap.String("positionSide", positionSide), zap.Float64("quantity", quantity), zap.Float64("price", price))
	return orderID, nil
}

// CreateSpotOrder 创建现货订单
func (o *OKXClient) CreateSpotOrder(ctx context.Context, symbol string, side string, orderType string, quantity float64, price float64) (string, error) {
	formattedSymbol := formatCCXTSpotSymbol(symbol)
	var ccxtType string
	var priceOpt ccxt.CreateOrderOptions
	if strings.ToUpper(orderType) == "MARKET" {
		ccxtType = "market"
	} else {
		ccxtType = "limit"
		if price <= 0 {
			return "", fmt.Errorf("limit order requires a positive price")
		}
		priceOpt = ccxt.WithCreateOrderPrice(price)
	}
	var ccxtSide string
	if strings.ToUpper(side) == "BUY" {
		ccxtSide = "buy"
	} else if strings.ToUpper(side) == "SELL" {
		ccxtSide = "sell"
	} else {
		return "", fmt.Errorf("invalid side: %s", side)
	}

	params := map[string]interface{}{"tdMode": "cash"}
	paramsOpt := ccxt.WithCreateOrderParams(params)

	var opts []ccxt.CreateOrderOptions
	if priceOpt != nil {
		opts = append(opts, priceOpt)
	}
	opts = append(opts, paramsOpt)

	orderResult, err := o.exchange.CreateOrder(formattedSymbol, ccxtType, ccxtSide, quantity, opts...)
	if err != nil {
		o.logger.Error("创建OKX现货订单失败", zap.Error(err), zap.String("symbol", symbol), zap.String("side", side), zap.Float64("quantity", quantity), zap.Float64("price", price))
		return "", fmt.Errorf("创建OKX现货订单失败: %w", err)
	}

	if orderResult.Id == nil || *orderResult.Id == "" {
		o.logger.Error("无法从OKX现货订单结果中提取订单ID", zap.Any("orderResult", orderResult))
		return "", fmt.Errorf("无法从OKX现货订单结果中提取订单ID")
	}
	orderID := *orderResult.Id

	o.logger.Info("成功创建OKX现货订单", zap.String("orderID", orderID), zap.String("symbol", symbol), zap.String("side", side), zap.Float64("quantity", quantity), zap.Float64("price", price))
	return orderID, nil
}

// GetBalance 获取账户资产余额
func (o *OKXClient) GetBalance(ctx context.Context, asset string) (float64, error) {
	balance, err := o.exchange.FetchBalance()
	if err != nil {
		o.logger.Error("获取OKX余额失败", zap.Error(err), zap.String("asset", asset))
		return 0, fmt.Errorf("获取OKX余额失败: %w", err)
	}

	if balance.Balances == nil {
		o.logger.Warn("FetchBalance 返回了 nil balance 对象", zap.String("asset", asset))
		return 0, fmt.Errorf("获取OKX余额失败：返回了 nil balance 对象")
	}

	assetUpper := strings.ToUpper(asset)

	if balance.Total != nil {
		if val, ok := balance.Total[assetUpper]; ok && val != nil {
			return *val, nil
		}
	}

	freeBalance := 0.0
	usedBalance := 0.0
	found := false
	if balance.Free != nil {
		if val, ok := balance.Free[assetUpper]; ok && val != nil {
			freeBalance = *val
			found = true
		}
	}
	if balance.Used != nil {
		if val, ok := balance.Used[assetUpper]; ok && val != nil {
			usedBalance = *val
			found = true
		}
	}

	if found {
		return freeBalance + usedBalance, nil
	}

	o.logger.Warn("未找到OKX资产余额", zap.String("asset", asset))
	return 0, fmt.Errorf("未找到资产 %s 的余额", asset)
}

// PlaceOrder 下单通用方法 (符合 Exchange 接口)
func (o *OKXClient) PlaceOrder(ctx context.Context, symbol, side, orderType string, quantity, price float64) (string, error) {
	if strings.Contains(strings.ToLower(orderType), "contract") || strings.Contains(strings.ToLower(orderType), "future") || strings.Contains(strings.ToLower(orderType), "swap") {
		o.logger.Warn("PlaceOrder (interface) called for OKX contract without positionSide, defaulting to LONG",
			zap.String("symbol", symbol),
			zap.String("orderType", orderType))
		formattedSymbol := formatCCXTSymbol(symbol)
		var contractOrderType string
		if strings.Contains(strings.ToUpper(orderType), "MARKET") {
			contractOrderType = "MARKET"
		} else {
			contractOrderType = "LIMIT"
		}
		return o.CreateContractOrder(ctx, formattedSymbol, side, "long", contractOrderType, quantity, price)
	} else {
		formattedSymbol := formatCCXTSpotSymbol(symbol)
		var spotOrderType string
		if strings.Contains(strings.ToUpper(orderType), "MARKET") {
			spotOrderType = "MARKET"
		} else {
			spotOrderType = "LIMIT"
		}
		return o.CreateSpotOrder(ctx, formattedSymbol, side, spotOrderType, quantity, price)
	}
}

// GetOrderStatus 获取订单状态
func (o *OKXClient) GetOrderStatus(ctx context.Context, symbol, orderID string) (string, error) {
	var formattedSymbol string
	var instType string
	if strings.Contains(symbol, "/") {
		if strings.Contains(symbol, "SWAP") || strings.Contains(symbol, "PERP") {
			formattedSymbol = formatCCXTSymbol(symbol)
			instType = "SWAP"
		} else {
			formattedSymbol = formatCCXTSpotSymbol(symbol)
			instType = "SPOT"
		}
	} else {
		formattedSymbol = symbol
		if strings.Contains(symbol, "-SWAP") {
			instType = "SWAP"
		} else {
			instType = "SPOT"
		}
	}

	params := map[string]interface{}{"instType": instType}
	symbolOpt := ccxt.WithFetchOrderSymbol(formattedSymbol)
	paramsOpt := ccxt.WithFetchOrderParams(params)

	order, err := o.exchange.FetchOrder(orderID, symbolOpt, paramsOpt)
	if err != nil {
		o.logger.Error("获取OKX订单状态失败", zap.Error(err), zap.String("symbol", symbol), zap.String("orderID", orderID))
		return "", fmt.Errorf("获取OKX订单状态失败 for %s: %w", orderID, err)
	}

	if order.Status == nil {
		o.logger.Warn("OKX订单状态未知", zap.String("orderID", orderID), zap.Any("orderInfo", order))
		return "", fmt.Errorf("订单状态未知 for %s", orderID)
	}
	return *order.Status, nil
}

// 辅助函数：将BTC/USDT格式的交易对转换为CCXT使用的格式
func formatCCXTSymbol(symbol string) string {
	parts := strings.Split(symbol, "/")
	if len(parts) == 2 {
		return fmt.Sprintf("%s-%s-SWAP", parts[0], parts[1])
	}
	return symbol
}

// 辅助函数：将BTC/USDT格式的交易对转换为CCXT现货格式
func formatCCXTSpotSymbol(symbol string) string {
	parts := strings.Split(symbol, "/")
	if len(parts) == 2 {
		return fmt.Sprintf("%s-%s", parts[0], parts[1])
	}
	return symbol
}

// 辅助函数：将CCXT格式的交易对转换回标准格式
func formatStandardSymbol(symbol string) string {
	symbol = strings.Replace(symbol, "-SWAP", "", 1)
	parts := strings.Split(symbol, "-")
	if len(parts) >= 2 {
		return fmt.Sprintf("%s/%s", parts[0], parts[1])
	}
	return symbol
}

// 辅助函数：将BTC/USDT格式的交易对转换为OKX使用的格式
func formatOKXSymbol(symbol string) string {
	parts := strings.Split(symbol, "/")
	if len(parts) == 2 {
		return fmt.Sprintf("%s-%s-SWAP", parts[0], parts[1])
	}
	return symbol
}

// 辅助函数：将BTC/USDT格式的交易对转换为OKX现货格式
func formatOKXSpotSymbol(symbol string) string {
	parts := strings.Split(symbol, "/")
	if len(parts) == 2 {
		return fmt.Sprintf("%s-%s", parts[0], parts[1])
	}
	return symbol
}

// 辅助函数：将OKX格式的交易对转换回标准格式
func formatStandardOKXSymbol(symbol string) string {
	symbol = strings.Replace(symbol, "-SWAP", "", 1)
	parts := strings.Split(symbol, "-")
	if len(parts) >= 2 {
		return fmt.Sprintf("%s/%s", parts[0], parts[1])
	}
	return symbol
}

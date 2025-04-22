package exchange

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/life2you_mini/calcs/internal/config"

	ccxt "github.com/ccxt/ccxt/go/v4"
	"go.uber.org/zap"
	// "strconv" // strconv 似乎没有用到，可以注释掉
)

// BitgetClient Bitget交易所客户端
type BitgetClient struct {
	exchange *ccxt.Bitget // 确认类型为指针
	logger   *zap.Logger
	// 注意：Bitget 实现没有 markets 字段，如果需要加载市场信息，需要添加
}

// NewBitgetClient 创建新的Bitget客户端
func NewBitgetClient(apiKey, apiSecret, passphrase string, logger *zap.Logger, proxy *config.HttpProxyConfig) *BitgetClient {
	// 创建配置对象
	clientConfig := map[string]interface{}{
		"apiKey":          apiKey,
		"secret":          apiSecret,
		"password":        passphrase, // Bitget 使用 password 字段
		"enableRateLimit": true,
		"options": map[string]interface{}{
			"defaultType": "swap", // Bitget 可能默认为合约
		},
		"timeout": 30000, // 添加超时配置
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

	// 创建CCXT的Bitget实例
	bitgetInstance := ccxt.NewBitget(clientConfig)

	// 修正：传入 bitgetInstance 的地址
	return &BitgetClient{
		exchange: &bitgetInstance,
		logger:   logger,
	}
}

// GetExchangeName 获取交易所名称
func (b *BitgetClient) GetExchangeName() string {
	return "Bitget"
}

// GetFundingRate 获取指定交易对的资金费率
func (b *BitgetClient) GetFundingRate(ctx context.Context, symbol string) (*FundingRateData, error) {
	formattedSymbol := formatBitgetSymbol(symbol)
	params := map[string]interface{}{}
	fundingRateData, err := b.exchange.FetchFundingRate(formattedSymbol, ccxt.WithFetchFundingRateParams(params))
	if err != nil {
		b.logger.Error("获取Bitget资金费率失败", zap.Error(err), zap.String("symbol", symbol))
		return nil, fmt.Errorf("获取Bitget资金费率失败: %w", err)
	}
	if fundingRateData.FundingRate == nil {
		b.logger.Warn("Bitget资金费率数据或费率值为空", zap.String("symbol", symbol))
		return nil, fmt.Errorf("资金费率数据为空 for %s", symbol)
	}
	fundingRate := *fundingRateData.FundingRate
	var nextFundingTime int64
	if fundingRateData.NextFundingTimestamp != nil {
		nextFundingTime = int64(*fundingRateData.NextFundingTimestamp)
	} else {
		nextFundingTime = time.Now().Add(8 * time.Hour).UnixMilli()
	}
	result := &FundingRateData{
		Exchange:        b.GetExchangeName(),
		Symbol:          symbol,
		FundingRate:     fundingRate,
		YearlyRate:      CalculateYearlyRate(fundingRate),
		NextFundingTime: time.UnixMilli(nextFundingTime),
		Timestamp:       time.Now(),
	}
	return result, nil
}

// GetAllFundingRates 获取所有交易对的资金费率
func (b *BitgetClient) GetAllFundingRates(ctx context.Context) ([]*FundingRateData, error) {
	params := map[string]interface{}{}
	fundingRatesData, err := b.exchange.FetchFundingRates(ccxt.WithFetchFundingRatesParams(params))
	if err != nil {
		b.logger.Error("获取Bitget所有资金费率失败", zap.Error(err))
		return nil, fmt.Errorf("获取Bitget所有资金费率失败: %w", err)
	}
	if fundingRatesData.FundingRates == nil || len(fundingRatesData.FundingRates) == 0 {
		b.logger.Warn("Bitget FetchFundingRates 返回了 nil 数据或空的费率 map")
		return []*FundingRateData{}, nil
	}
	var result []*FundingRateData
	now := time.Now()
	for symbol, rate := range fundingRatesData.FundingRates {
		if rate.FundingRate == nil {
			continue
		}
		standardSymbol := formatStandardBitgetSymbol(symbol)
		var nextFundingTime int64
		if rate.NextFundingTimestamp != nil {
			nextFundingTime = int64(*rate.NextFundingTimestamp)
		} else {
			nextFundingTime = now.Add(8 * time.Hour).UnixMilli()
		}
		data := &FundingRateData{
			Exchange:        b.GetExchangeName(),
			Symbol:          standardSymbol,
			FundingRate:     *rate.FundingRate,
			YearlyRate:      CalculateYearlyRate(*rate.FundingRate),
			NextFundingTime: time.UnixMilli(nextFundingTime),
			Timestamp:       now,
		}
		result = append(result, data)
	}
	return result, nil
}

// GetMarketDepth 获取市场深度数据
func (b *BitgetClient) GetMarketDepth(ctx context.Context, symbol string) (map[string]interface{}, error) {
	formattedSymbol := formatBitgetSymbol(symbol)
	params := map[string]interface{}{}
	var contractOrderBook ccxt.OrderBook
	var spotOrderBook ccxt.OrderBook
	var contractErr, spotErr error

	contractOrderBook, contractErr = b.exchange.FetchOrderBook(formattedSymbol, ccxt.WithFetchOrderBookLimit(10), ccxt.WithFetchOrderBookParams(params))
	if contractErr != nil {
		b.logger.Error("获取Bitget合约深度失败", zap.Error(contractErr), zap.String("symbol", symbol))
	}

	spotSymbol := formatBitgetSpotSymbol(symbol)
	spotOrderBook, spotErr = b.exchange.FetchOrderBook(spotSymbol, ccxt.WithFetchOrderBookLimit(10), ccxt.WithFetchOrderBookParams(params))
	if spotErr != nil {
		b.logger.Error("获取Bitget现货深度失败", zap.Error(spotErr), zap.String("symbol", symbol))
	}

	contractHasData := len(contractOrderBook.Bids) > 0 || len(contractOrderBook.Asks) > 0
	spotHasData := len(spotOrderBook.Bids) > 0 || len(spotOrderBook.Asks) > 0

	if !contractHasData && !spotHasData {
		finalErr := spotErr
		if finalErr == nil {
			finalErr = contractErr
		}
		if finalErr == nil {
			finalErr = fmt.Errorf("无法获取 %s 的合约或现货市场深度数据", symbol)
		}
		return nil, fmt.Errorf("获取Bitget市场深度失败: %w", finalErr)
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
func (b *BitgetClient) GetPrice(ctx context.Context, symbol string) (float64, error) {
	formattedSymbol := formatBitgetSymbol(symbol)
	params := map[string]interface{}{}
	ticker, err := b.exchange.FetchTicker(formattedSymbol, ccxt.WithFetchTickerParams(params))
	if err != nil {
		b.logger.Error("获取Bitget价格失败", zap.Error(err), zap.String("symbol", symbol))
		return 0, fmt.Errorf("获取Bitget价格失败: %w", err)
	}
	if ticker.Last == nil {
		b.logger.Warn("Bitget价格数据为空", zap.String("symbol", symbol))
		return 0, fmt.Errorf("价格数据为空 for %s", symbol)
	}
	return *ticker.Last, nil
}

// SetLeverage 设置杠杆倍数 - **修正**
func (b *BitgetClient) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	params := map[string]interface{}{}

	// 使用 ccxt.WithSetLeverageParams 包装参数
	paramsOpt := ccxt.WithSetLeverageParams(params)

	// 调用 SetLeverage，使用 int64 和 options
	// 假设签名是 SetLeverage(symbol string, leverage int64, options ...SetLeverageOptions)
	_, err := b.exchange.SetLeverage(int64(leverage), paramsOpt)
	if err != nil {
		b.logger.Error("设置Bitget杠杆失败", zap.Error(err), zap.String("symbol", symbol), zap.Int("leverage", leverage))
		if ccxtErr, ok := err.(*ccxt.Error); ok {
			b.logger.Error("CCXT Error Type", zap.String("type", string(ccxtErr.Type)), zap.String("message", ccxtErr.Message))
		}
		return fmt.Errorf("设置Bitget杠杆失败: %w", err)
	}

	b.logger.Info("成功设置杠杆", zap.String("symbol", symbol), zap.Int("leverage", leverage))
	return nil
}

// CreateContractOrder 创建合约订单 - **修正**
func (b *BitgetClient) CreateContractOrder(ctx context.Context, symbol string, side string, positionSide string, orderType string, quantity float64, price float64) (string, error) {
	formattedSymbol := formatBitgetSymbol(symbol)
	var ccxtType string
	var priceOpt ccxt.CreateOrderOptions // 正确类型
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

	params := map[string]interface{}{"tdMode": "cross", "productType": "USDT-FUTURES"} // 添加 Bitget 特定参数
	paramsOpt := ccxt.WithCreateOrderParams(params)                                    // 正确类型

	var opts []ccxt.CreateOrderOptions // 正确类型
	if priceOpt != nil {
		opts = append(opts, priceOpt)
	}
	opts = append(opts, paramsOpt)

	order, err := b.exchange.CreateOrder(formattedSymbol, ccxtType, ccxtSide, quantity, opts...) // 传入 opts...
	if err != nil {
		b.logger.Error("创建Bitget合约订单失败", zap.Error(err), zap.String("symbol", symbol), zap.String("side", side), zap.String("positionSide", positionSide), zap.Float64("quantity", quantity), zap.Float64("price", price))
		if ccxtErr, ok := err.(*ccxt.Error); ok {
			b.logger.Error("CCXT Error Type", zap.String("type", string(ccxtErr.Type)), zap.String("message", ccxtErr.Message))
		}
		return "", fmt.Errorf("创建Bitget合约订单失败: %w", err)
	}
	if order.Id == nil || *order.Id == "" {
		b.logger.Error("无法从Bitget订单结果中提取订单ID", zap.Any("orderInfo", order))
		return "", fmt.Errorf("无法从Bitget订单结果中提取订单ID")
	}
	orderID := *order.Id
	b.logger.Info("成功创建Bitget合约订单", zap.String("orderID", orderID), zap.String("symbol", symbol), zap.String("side", side), zap.String("positionSide", positionSide), zap.Float64("quantity", quantity), zap.Float64("price", price))
	return orderID, nil
}

// CreateSpotOrder 创建现货订单 - **修正**
func (b *BitgetClient) CreateSpotOrder(ctx context.Context, symbol string, side string, orderType string, quantity float64, price float64) (string, error) {
	formattedSymbol := formatBitgetSpotSymbol(symbol) // 现货符号
	var ccxtType string
	var priceOpt ccxt.CreateOrderOptions // 正确类型
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

	params := map[string]interface{}{}              // 现货可能不需要特殊参数
	paramsOpt := ccxt.WithCreateOrderParams(params) // 正确类型

	var opts []ccxt.CreateOrderOptions // 正确类型
	if priceOpt != nil {
		opts = append(opts, priceOpt)
	}
	opts = append(opts, paramsOpt)

	order, err := b.exchange.CreateOrder(formattedSymbol, ccxtType, ccxtSide, quantity, opts...) // 传入 opts...
	if err != nil {
		b.logger.Error("创建Bitget现货订单失败", zap.Error(err), zap.String("symbol", symbol), zap.String("side", side), zap.Float64("quantity", quantity), zap.Float64("price", price))
		if ccxtErr, ok := err.(*ccxt.Error); ok {
			b.logger.Error("CCXT Error Type", zap.String("type", string(ccxtErr.Type)), zap.String("message", ccxtErr.Message))
		}
		return "", fmt.Errorf("创建Bitget现货订单失败: %w", err)
	}
	if order.Id == nil || *order.Id == "" {
		b.logger.Error("无法从Bitget现货订单结果中提取订单ID", zap.Any("orderInfo", order))
		return "", fmt.Errorf("无法从Bitget现货订单结果中提取订单ID")
	}
	orderID := *order.Id
	b.logger.Info("成功创建Bitget现货订单", zap.String("orderID", orderID), zap.String("symbol", symbol), zap.String("side", side), zap.Float64("quantity", quantity), zap.Float64("price", price))
	return orderID, nil
}

// GetBalance 获取账户资产余额
func (b *BitgetClient) GetBalance(ctx context.Context, asset string) (float64, error) {
	balance, err := b.exchange.FetchBalance()
	if err != nil {
		b.logger.Error("获取Bitget余额失败", zap.Error(err), zap.String("asset", asset))
		return 0, fmt.Errorf("获取Bitget余额失败: %w", err)
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
	b.logger.Warn("未找到Bitget资产余额", zap.String("asset", asset))
	return 0, fmt.Errorf("未找到资产 %s 的余额", asset)
}

// PlaceOrder 下单通用方法
func (b *BitgetClient) PlaceOrder(ctx context.Context, symbol, side, orderType string, quantity, price float64) (string, error) {
	// 这个逻辑可能需要调整，因为合约下单需要 positionSide 或其他参数
	if strings.Contains(strings.ToLower(orderType), "contract") { // 简单判断，可能不准确
		// 需要确定 positionSide 或其他合约特定参数
		return b.CreateContractOrder(ctx, symbol, side, "LONG", orderType, quantity, price) // 示例用 LONG
	}
	return b.CreateSpotOrder(ctx, symbol, side, orderType, quantity, price)
}

// GetOrderStatus 获取订单状态 - **修正**
func (b *BitgetClient) GetOrderStatus(ctx context.Context, symbol, orderID string) (string, error) {
	formattedSymbol := formatBitgetSymbol(symbol) // 或 spot 符号
	params := map[string]interface{}{
		// Bitget 可能需要 'productType': 'USDT-FUTURES' 或 'SPOT'
		"productType": "USDT-FUTURES", // 假设是查询合约订单
	}
	// 使用 With... 函数
	symbolOpt := ccxt.WithFetchOrderSymbol(formattedSymbol) // 正确类型
	paramsOpt := ccxt.WithFetchOrderParams(params)          // 正确类型

	order, err := b.exchange.FetchOrder(orderID, symbolOpt, paramsOpt) // 传入 opts...
	if err != nil {
		b.logger.Error("使用 symbol 获取 Bitget 订单状态失败", zap.Error(err), zap.String("symbol", symbol), zap.String("orderID", orderID))
		b.logger.Info("尝试不带 symbol 获取 Bitget 订单", zap.String("orderID", orderID))
		order, err = b.exchange.FetchOrder(orderID, paramsOpt) // 传入 opts...
		if err != nil {
			b.logger.Error("不带 symbol 获取 Bitget 订单状态仍然失败", zap.Error(err), zap.String("orderID", orderID))
			if ccxtErr, ok := err.(*ccxt.Error); ok {
				b.logger.Error("CCXT Error Type", zap.String("type", string(ccxtErr.Type)), zap.String("message", ccxtErr.Message))
			}
			return "", fmt.Errorf("获取 Bitget 订单状态失败 for %s: %w", orderID, err)
		}
	}
	if order.Status == nil {
		b.logger.Warn("Bitget 订单状态未知", zap.String("orderID", orderID), zap.Any("orderInfo", order))
		return "", fmt.Errorf("订单状态未知 for %s", orderID)
	}
	return *order.Status, nil
}

// 辅助函数 formatBitgetSymbol
func formatBitgetSymbol(symbol string) string {
	base := strings.ReplaceAll(symbol, "/", "")
	if strings.HasSuffix(base, "USDT") {
		return base + "_UMCBL" // 假设默认U本位
	}
	return base
}

// 辅助函数 formatBitgetSpotSymbol
func formatBitgetSpotSymbol(symbol string) string {
	return strings.ToUpper(strings.ReplaceAll(symbol, "/", ""))
}

// 辅助函数 formatStandardBitgetSymbol
func formatStandardBitgetSymbol(symbol string) string {
	if strings.HasSuffix(symbol, "_UMCBL") {
		base := strings.TrimSuffix(symbol, "_UMCBL")
		if len(base) > 4 && base[len(base)-4:] == "USDT" {
			return fmt.Sprintf("%s/USDT", base[:len(base)-4])
		}
	} else if strings.HasSuffix(symbol, "_DMCBL") {
		base := strings.TrimSuffix(symbol, "_DMCBL")
		if len(base) > 3 && base[len(base)-3:] == "USD" {
			return fmt.Sprintf("%s/USD", base[:len(base)-3])
		}
	} else if len(symbol) > 4 && symbol[len(symbol)-4:] == "USDT" {
		return fmt.Sprintf("%s/USDT", symbol[:len(symbol)-4])
	}
	return symbol
}

// 注意：可能需要 GetMinNotional 和 LoadMarkets 的 Bitget 实现

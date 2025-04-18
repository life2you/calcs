package exchange

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	ccxt "github.com/ccxt/ccxt/go/v4"
	"go.uber.org/zap"
)

// BinanceClient 币安交易所客户端
type BinanceClient struct {
	exchange ccxt.Binance
	logger   *zap.Logger
	markets  []ccxt.MarketInterface
}

// NewBinanceClient 创建新的币安客户端
func NewBinanceClient(apiKey, apiSecret string, logger *zap.Logger) *BinanceClient {
	binanceInstance := ccxt.NewBinance(map[string]interface{}{
		"apiKey":          apiKey,
		"secret":          apiSecret,
		"enableRateLimit": true,
		"options": map[string]interface{}{
			"defaultType": "future",
		},
	})

	client := &BinanceClient{
		exchange: binanceInstance,
		logger:   logger,
	}

	err := client.LoadMarkets()
	if err != nil {
		logger.Fatal("初始化时加载Binance市场数据失败", zap.Error(err))
	} else {
		logger.Info("Binance市场数据加载完成")
	}

	return client
}

// GetExchangeName 获取交易所名称
func (b *BinanceClient) GetExchangeName() string {
	return "Binance"
}

// GetFundingRate 获取指定交易对的资金费率
func (b *BinanceClient) GetFundingRate(ctx context.Context, symbol string) (*FundingRateData, error) {
	formattedSymbol := formatBinanceSymbol(symbol)
	params := map[string]interface{}{}

	fundingRateData, err := b.exchange.FetchFundingRate(formattedSymbol, ccxt.WithFetchFundingRateParams(params))
	if err != nil {
		b.logger.Error("获取币安资金费率失败", zap.Error(err), zap.String("symbol", symbol))
		return nil, fmt.Errorf("获取币安资金费率失败: %w", err)
	}

	// 检查 FundingRate 字段是否为 nil
	if fundingRateData.FundingRate == nil {
		b.logger.Warn("资金费率数据或费率值为空", zap.String("symbol", symbol))
		return nil, fmt.Errorf("资金费率数据为空 for %s", symbol)
	}
	fundingRate := *fundingRateData.FundingRate

	var nextFundingTime int64
	if fundingRateData.NextFundingTimestamp != nil {
		nextFundingTime = int64(*fundingRateData.NextFundingTimestamp)
	} else {
		// Info 字段不存在，直接使用默认逻辑
		b.logger.Warn("NextFundingTimestamp 为 nil，使用默认8小时", zap.String("symbol", symbol))
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
func (b *BinanceClient) GetAllFundingRates(ctx context.Context) ([]*FundingRateData, error) {
	params := map[string]interface{}{}

	fundingRatesData, err := b.exchange.FetchFundingRates(ccxt.WithFetchFundingRatesParams(params))
	if err != nil {
		b.logger.Error("获取币安所有资金费率失败", zap.Error(err))
		return nil, fmt.Errorf("获取币安所有资金费率失败: %w", err)
	}

	// 检查 FundingRates map 是否为 nil 或空
	if fundingRatesData.FundingRates == nil || len(fundingRatesData.FundingRates) == 0 {
		b.logger.Warn("FetchFundingRates 返回了 nil 数据或空的费率 map")
		return []*FundingRateData{}, nil // 返回空切片
	}

	var result []*FundingRateData
	now := time.Now()
	for symbol, rate := range fundingRatesData.FundingRates {
		// 检查 map 中的 rate 指针
		if rate.FundingRate == nil {
			continue
		} // 跳过费率为 nil 的

		standardSymbol := formatStandardBinanceSymbol(symbol)
		var nextFundingTime int64
		if rate.NextFundingTimestamp != nil {
			nextFundingTime = int64(*rate.NextFundingTimestamp)
		} else {
			// Info 字段不存在，直接使用默认逻辑
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
func (b *BinanceClient) GetMarketDepth(ctx context.Context, symbol string) (map[string]interface{}, error) {
	futureSymbol := formatBinanceSymbol(symbol)
	params := map[string]interface{}{}
	var contractOrderBook ccxt.OrderBook
	var spotOrderBook ccxt.OrderBook
	var contractErr, spotErr error

	// 获取合约市场深度
	contractOrderBook, contractErr = b.exchange.FetchOrderBook(futureSymbol, ccxt.WithFetchOrderBookLimit(10), ccxt.WithFetchOrderBookParams(params))
	if contractErr != nil {
		b.logger.Error("获取币安合约深度失败", zap.Error(contractErr), zap.String("symbol", symbol))
	}

	spotSymbol := formatBinanceSpotSymbol(symbol)
	// 获取现货市场深度
	spotOrderBook, spotErr = b.exchange.FetchOrderBook(spotSymbol, ccxt.WithFetchOrderBookLimit(10), ccxt.WithFetchOrderBookParams(params))
	if spotErr != nil {
		b.logger.Error("获取币安现货深度失败", zap.Error(spotErr), zap.String("symbol", symbol))
	}

	// 检查是否有有效数据（Bids 或 Asks 不为空）
	contractHasData := len(contractOrderBook.Bids) > 0 || len(contractOrderBook.Asks) > 0
	spotHasData := len(spotOrderBook.Bids) > 0 || len(spotOrderBook.Asks) > 0

	if !contractHasData && !spotHasData {
		// 如果两个都失败或都没有数据，返回最后一个错误（或组合错误）
		finalErr := spotErr // 默认使用现货错误
		if finalErr == nil {
			finalErr = contractErr // 如果现货没错误，使用合约错误
		}
		if finalErr == nil { // 如果两个都没错误但都没数据
			finalErr = fmt.Errorf("无法获取 %s 的合约或现货市场深度数据", symbol)
		}
		return nil, fmt.Errorf("获取市场深度失败: %w", finalErr)
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
func (b *BinanceClient) GetPrice(ctx context.Context, symbol string) (float64, error) {
	formattedSymbol := formatBinanceSymbol(symbol)
	params := map[string]interface{}{}

	ticker, err := b.exchange.FetchTicker(formattedSymbol, ccxt.WithFetchTickerParams(params))
	if err != nil {
		b.logger.Error("获取币安价格失败", zap.Error(err), zap.String("symbol", symbol))
		return 0, fmt.Errorf("获取币安价格失败: %w", err)
	}

	// 检查 ticker 本身和 Last 字段
	if ticker.Last == nil {
		b.logger.Warn("价格数据为空", zap.String("symbol", symbol))
		return 0, fmt.Errorf("价格数据为空 for %s", symbol)
	}

	return *ticker.Last, nil
}

// SetLeverage 设置合约杠杆
func (b *BinanceClient) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	params := map[string]interface{}{}

	_, err := b.exchange.SetLeverage(int64(leverage), ccxt.WithSetLeverageParams(params))
	if err != nil {
		b.logger.Error("设置币安杠杆失败",
			zap.Error(err),
			zap.String("symbol", symbol),
			zap.Int("leverage", leverage))
		if ccxtErr, ok := err.(*ccxt.Error); ok {
			b.logger.Error("CCXT Error Type", zap.String("type", string(ccxtErr.Type)), zap.String("message", ccxtErr.Message))
		}
		return fmt.Errorf("设置币安杠杆失败: %w", err)
	}

	b.logger.Info("成功设置杠杆", zap.String("symbol", symbol), zap.Int("leverage", leverage))
	return nil
}

// CreateContractOrder 创建合约订单
func (b *BinanceClient) CreateContractOrder(ctx context.Context, symbol string, side string, positionSide string, orderType string, quantity float64, price float64) (string, error) {
	formattedSymbol := formatBinanceSymbol(symbol)
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
	params := map[string]interface{}{"positionSide": strings.ToUpper(positionSide)}
	paramsOpt := ccxt.WithCreateOrderParams(params)
	var opts []ccxt.CreateOrderOptions
	if priceOpt != nil {
		opts = append(opts, priceOpt)
	}
	opts = append(opts, paramsOpt)

	order, err := b.exchange.CreateOrder(formattedSymbol, ccxtType, ccxtSide, quantity, opts...)
	if err != nil {
		b.logger.Error("创建币安合约订单失败", zap.Error(err), zap.String("symbol", symbol), zap.String("side", side), zap.String("positionSide", positionSide), zap.Float64("quantity", quantity), zap.Float64("price", price))
		if ccxtErr, ok := err.(*ccxt.Error); ok {
			b.logger.Error("CCXT Error Type", zap.String("type", string(ccxtErr.Type)), zap.String("message", ccxtErr.Message))
		}
		return "", fmt.Errorf("创建币安合约订单失败: %w", err)
	}
	// 检查返回的 order 指针和 Id 字段
	if order.Id == nil || *order.Id == "" {
		b.logger.Error("无法从币安订单结果中提取订单ID", zap.Any("orderInfo", order)) // Log order info for debug
		return "", fmt.Errorf("无法从币安订单结果中提取订单ID")
	}
	orderID := *order.Id
	b.logger.Info("成功创建币安合约订单", zap.String("orderID", orderID), zap.String("symbol", symbol), zap.String("side", side), zap.String("positionSide", positionSide), zap.Float64("quantity", quantity), zap.Float64("price", price))
	return orderID, nil
}

// CreateSpotOrder 创建现货订单
func (b *BinanceClient) CreateSpotOrder(ctx context.Context, symbol string, side string, orderType string, quantity float64, price float64) (string, error) {
	formattedSymbol := formatBinanceSpotSymbol(symbol)
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
	params := map[string]interface{}{}
	paramsOpt := ccxt.WithCreateOrderParams(params)
	var opts []ccxt.CreateOrderOptions
	if priceOpt != nil {
		opts = append(opts, priceOpt)
	}
	opts = append(opts, paramsOpt)

	order, err := b.exchange.CreateOrder(formattedSymbol, ccxtType, ccxtSide, quantity, opts...)
	if err != nil {
		b.logger.Error("创建币安现货订单失败", zap.Error(err), zap.String("symbol", symbol), zap.String("side", side), zap.Float64("quantity", quantity), zap.Float64("price", price))
		if ccxtErr, ok := err.(*ccxt.Error); ok {
			b.logger.Error("CCXT Error Type", zap.String("type", string(ccxtErr.Type)), zap.String("message", ccxtErr.Message))
		}
		return "", fmt.Errorf("创建币安现货订单失败: %w", err)
	}
	// 检查返回的 order 指针和 Id 字段
	if order.Id == nil || *order.Id == "" {
		b.logger.Error("无法从币安现货订单结果中提取订单ID", zap.Any("orderInfo", order)) // Log order info for debug
		return "", fmt.Errorf("无法从币安现货订单结果中提取订单ID")
	}
	orderID := *order.Id
	b.logger.Info("成功创建币安现货订单", zap.String("orderID", orderID), zap.String("symbol", symbol), zap.String("side", side), zap.Float64("quantity", quantity), zap.Float64("price", price))
	return orderID, nil
}

// 辅助函数：将BTC/USDT格式的交易对转换为Binance合约使用的格式
func formatBinanceSymbol(symbol string) string {
	return strings.ReplaceAll(symbol, "/", "")
}

// 辅助函数：将BTC/USDT格式的交易对转换为Binance现货格式
func formatBinanceSpotSymbol(symbol string) string {
	return strings.ReplaceAll(symbol, "/", "")
}

// 辅助函数：将Binance格式的交易对转换回标准格式
func formatStandardBinanceSymbol(symbol string) string {
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		if !strings.Contains(base, "/") {
			return fmt.Sprintf("%s/USDT", base)
		}
	} else if strings.HasSuffix(symbol, "BTC") {
		base := strings.TrimSuffix(symbol, "BTC")
		if !strings.Contains(base, "/") {
			return fmt.Sprintf("%s/BTC", base)
		}
	}
	return symbol
}

// GetBalance 获取账户资产余额
func (b *BinanceClient) GetBalance(ctx context.Context, asset string) (float64, error) {
	balance, err := b.exchange.FetchBalance()
	if err != nil {
		b.logger.Error("获取币安余额失败", zap.Error(err), zap.String("asset", asset))
		return 0, fmt.Errorf("获取币安余额失败: %w", err)
	}
	// 检查返回的 balance 指针
	if balance.Balances == nil {
		b.logger.Warn("FetchBalance 返回了 nil balance 对象", zap.String("asset", asset))
		return 0, fmt.Errorf("获取币安余额失败：返回了 nil balance 对象")
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
	b.logger.Warn("未找到资产余额", zap.String("asset", asset))
	return 0, fmt.Errorf("未找到资产 %s 的余额", asset)
}

// PlaceOrder 实现 - **修正签名以匹配 Exchange 接口**
func (b *BinanceClient) PlaceOrder(ctx context.Context, symbol, side, orderType string, quantity, price float64) (string, error) {
	// 由于接口没有 positionSide，我们需要在这里决定逻辑
	// 方案2：尝试根据 orderType 猜测，合约默认 positionSide 为 "BOTH"
	if strings.Contains(strings.ToLower(orderType), "contract") || strings.Contains(strings.ToLower(orderType), "future") || strings.Contains(strings.ToLower(orderType), "swap") {
		b.logger.Warn("PlaceOrder (interface) called for contract without positionSide, defaulting to BOTH",
			zap.String("symbol", symbol),
			zap.String("orderType", orderType))
		var contractOrderType string
		if strings.Contains(strings.ToUpper(orderType), "MARKET") {
			contractOrderType = "MARKET"
		} else {
			contractOrderType = "LIMIT"
		}
		return b.CreateContractOrder(ctx, symbol, side, "BOTH", contractOrderType, quantity, price)
	} else {
		// 默认为现货
		var spotOrderType string
		if strings.Contains(strings.ToUpper(orderType), "MARKET") {
			spotOrderType = "MARKET"
		} else {
			spotOrderType = "LIMIT"
		}
		return b.CreateSpotOrder(ctx, symbol, side, spotOrderType, quantity, price)
	}
}

// GetOrderStatus 获取订单状态
func (b *BinanceClient) GetOrderStatus(ctx context.Context, symbol, orderID string) (string, error) {
	formattedSymbol := formatBinanceSymbol(symbol)
	params := map[string]interface{}{}
	symbolOpt := ccxt.WithFetchOrderSymbol(formattedSymbol)
	paramsOpt := ccxt.WithFetchOrderParams(params)

	order, err := b.exchange.FetchOrder(orderID, symbolOpt, paramsOpt)
	if err != nil {
		b.logger.Error("使用 symbol 获取订单状态失败", zap.Error(err), zap.String("symbol", symbol), zap.String("orderID", orderID))
		b.logger.Info("尝试不带 symbol 获取订单", zap.String("orderID", orderID))
		order, err = b.exchange.FetchOrder(orderID, paramsOpt)
		if err != nil {
			b.logger.Error("不带 symbol 获取订单状态仍然失败", zap.Error(err), zap.String("orderID", orderID))
			if ccxtErr, ok := err.(*ccxt.Error); ok {
				b.logger.Error("CCXT Error Type", zap.String("type", string(ccxtErr.Type)), zap.String("message", ccxtErr.Message))
			}
			return "", fmt.Errorf("获取订单状态失败 for %s: %w", orderID, err)
		}
	}
	// 检查返回的 order 指针和 Status 字段
	if order.Status == nil {
		b.logger.Warn("订单状态未知 或 order 为 nil", zap.String("orderID", orderID), zap.Any("orderInfo", order)) // Log order info
		return "", fmt.Errorf("订单状态未知 for %s", orderID)
	}

	return *order.Status, nil
}

// GetMinNotional 实现 - **修正 Linter 错误**
func (b *BinanceClient) GetMinNotional(symbol string) (float64, error) {
	if b.markets == nil {
		b.logger.Warn("Markets 未加载，尝试重新加载")
		err := b.LoadMarkets()
		if err != nil {
			return 0, fmt.Errorf("获取最小名义价值前加载市场信息失败: %w", err)
		}
		if b.markets == nil {
			return 0, fmt.Errorf("加载市场信息后 markets 仍然为 nil")
		}
	}
	formattedSymbol := formatBinanceSymbol(symbol)
	var targetMarket ccxt.MarketInterface
	foundMarket := false
	for _, market := range b.markets {
		// 修正：访问 Symbol 前无需检查 market != nil (因为接口类型自身非 nil)
		if market.Symbol != nil && *market.Symbol == formattedSymbol {
			targetMarket = market
			foundMarket = true
			break
		}
	}

	if !foundMarket {
		err := b.LoadMarkets()
		if err != nil {
			b.logger.Error("重新加载市场信息失败", zap.Error(err))
		}
		for _, market := range b.markets {
			// 修正：访问 Symbol 前无需检查 market != nil (因为接口类型自身非 nil)
			if market.Symbol != nil && *market.Symbol == formattedSymbol {
				targetMarket = market
				foundMarket = true
				break
			}
		}
		if !foundMarket {
			return 0, fmt.Errorf("未找到 %s 的市场信息", symbol)
		}
	}

	// 访问 Info - 修正：直接访问 Info 字段
	marketInfo := targetMarket.Info

	// 修正：检查 Info map 是否为 nil
	if marketInfo == nil {
		b.logger.Warn("无法获取市场详细信息(Info map is nil)", zap.String("symbol", symbol), zap.Any("targetMarketType", fmt.Sprintf("%T", targetMarket)))
		return 0, fmt.Errorf("无法获取 %s 的市场详细信息 (Info map is nil)", symbol)
	}

	if minNotionalRaw, ok := marketInfo["minNotional"]; ok {
		switch minVal := minNotionalRaw.(type) {
		case float64:
			return minVal, nil
		case string:
			minFloat, err := strconv.ParseFloat(minVal, 64)
			if err == nil {
				return minFloat, nil
			}
			b.logger.Error("无法将 minNotional 从字符串转换为 float64", zap.String("minString", minVal), zap.Error(err))
		default:
			b.logger.Warn("未知的 minNotional 类型", zap.Any("minNotionalRaw", minNotionalRaw), zap.String("symbol", symbol))
		}
	}
	if limitsRaw, ok := marketInfo["limits"]; ok {
		if limitsMap, ok := limitsRaw.(map[string]interface{}); ok {
			if costRaw, ok := limitsMap["cost"]; ok {
				if costMap, ok := costRaw.(map[string]interface{}); ok {
					if minRaw, ok := costMap["min"]; ok {
						switch minVal := minRaw.(type) {
						case float64:
							return minVal, nil
						case string:
							minFloat, err := strconv.ParseFloat(minVal, 64)
							if err == nil {
								return minFloat, nil
							}
							b.logger.Error("无法将最小名义价值(limits.cost.min)从字符串转换为float64", zap.String("minString", minVal), zap.Error(err))
							return 0, fmt.Errorf("无法解析最小名义价值 '%s': %w", minVal, err)
						default:
							b.logger.Warn("未知的最小名义价值(limits.cost.min)类型", zap.Any("minRaw", minRaw), zap.String("symbol", symbol))
							return 0, fmt.Errorf("未知的最小名义价值类型 for %s", symbol)
						}
					}
				}
			}
		}
	}
	b.logger.Warn("无法从市场信息中找到最小名义价值(minNotional or limits.cost.min)", zap.String("symbol", symbol))
	return 0, fmt.Errorf("无法找到 %s 的最小名义价值信息", symbol)
}

// LoadMarkets 加载市场信息
func (b *BinanceClient) LoadMarkets() error {
	marketsSlice, err := b.exchange.FetchMarkets()
	if err != nil {
		b.logger.Error("加载Binance市场数据失败", zap.Error(err))
		return fmt.Errorf("加载Binance市场数据失败: %w", err)
	}
	if marketsSlice == nil {
		b.logger.Error("FetchMarkets 返回了 nil slice")
		return fmt.Errorf("加载Binance市场数据失败: 返回了 nil slice")
	}
	b.markets = marketsSlice // 赋值给切片类型
	b.logger.Info("成功加载或更新市场信息", zap.Int("market_count", len(b.markets)))
	return nil
}

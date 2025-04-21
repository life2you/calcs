# 更新日志

所有项目的显著变更都将记录在此文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
并且本项目遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [未发布]

### 已添加
- 配置管理模块，支持从YAML文件加载配置
- 交易所接口定义及工厂模式实现
- 基于CCXT的币安(Binance)、OKX和Bitget交易所实现
- 资金费率监控模块，支持多交易所资金费率获取
- Redis存储模块，包括资金费率历史数据存储和队列系统
- 系统基础设施，包含服务启动/关闭逻辑和信号处理
- 交易执行模块基础实现，包括：
  - 交易队列处理器
  - 交易决策逻辑
  - 交易规模计算算法
  - 杠杆优化逻辑
  - 持仓管理基础结构
  - 模拟交易执行流程
- 风险管理模块实现，包括：
  - 风险监控队列处理器
  - 清算风险和对冲偏差监控
  - 持仓时间风险评估
  - 分级风险处理策略
  - 风险缓解建议生成

### 待添加
- 交易执行模块完整实现（与交易所API集成）
- 数据分析与优化功能
- 通知系统
- PostgreSQL持久化存储
- 用户界面/API

## [0.1.0] - 2023-06-15

### 已添加
- 项目初始结构设计
- 基本需求分析和架构规划

## [日期] - 代码修复尝试

- 尝试修复 `internal/exchange/binance.go` 中的编译错误，涉及 `SetLeverage`, `CreateOrder`, `FetchOrder` 函数的参数类型和 `GetMinNotional` 中对 `market.Info` 的访问。
- 初步尝试使用假设的 Options 结构体失败。
- 再次尝试恢复使用 `map[string]interface{}` 作为参数，并调整类型转换和顺序，但仍遇到编译错误。
- **结论:** 需要确认所使用的 `ccxt` 库的具体函数签名才能继续修复。

## 20XX-XX-XX 代码结构优化

### 修复
- 解决了 `FundingRateData` 类型重复定义的问题，统一使用 `model.FundingRateData`
- 修复了 `Position` 结构体重复定义问题，合并到 `position.go` 文件
- 修正了接口使用错误，将指针类型 `*storage.RedisClient` 修改为接口类型 `storage.RedisClient`
- 在 `RedisClient` 接口中添加了缺失的 `PopFromTradeQueue` 方法定义
- 修复了 `ClosePosition` 函数中的类型错误，正确处理 `PnL` 字段
- 修正了 `trader.go` 中对 Exchange 工厂的调用方式，使用 `Get` 而不是 `GetExchange`
- 修复了 `trader.go` 中对 FundingRateData 字段的访问方式
- 在 `TradeDecision` 结构体中添加了缺失的字段 (ContractSide, ContractPosSide, SpotSide, FundingRate)
- 修复了未使用的 `direction` 变量问题，使用它来确定交易方向
- 修正了 `balance` 的使用方式，适配 `GetBalance` 方法返回的 float64 类型
- 修正了 `exchangeFactory.Get` 返回值的检查，使用返回的布尔值而不是错误检查

### 待解决
- `services` 包中的类型不匹配问题，包括 `ExchangeAPI` 接口实现和 `RedisClientInterface` 接口实现
- `risk` 包中的字段错误，`Position` 结构体字段访问不正确

(这里将记录每次代码的变动) 
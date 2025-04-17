# 合约资金费率套利系统

这是一个基于Go语言开发的加密货币合约资金费率套利系统，通过监控多个交易所的资金费率，自动识别套利机会并执行交易。

## 功能特点

- 支持多个主流交易所：Binance、OKX、Bitget
- 使用CCXT框架统一交易所API调用
- 自动监控资金费率，识别高收益机会
- 风险管理系统，维护头寸安全
- Redis队列设计，提高系统可靠性
- 提供详细的交易历史和盈利报告

## 项目开发状态

### 已开发完成的功能

- ✅ **配置管理**
  - YAML配置文件加载实现
  - 配置验证逻辑
  - 命令行参数处理

- ✅ **交易所接口**
  - 统一的交易所接口定义
  - 基于CCXT的币安(Binance)实现
  - 基于CCXT的OKX实现
  - 基于CCXT的Bitget实现
  - 交易所工厂模式实现

- ✅ **资金费率监控**
  - 从多交易所获取资金费率数据
  - 资金费率筛选与年化收益计算
  - 套利机会识别

- ✅ **存储系统**
  - Redis客户端实现
  - 资金费率历史数据存储
  - 队列系统实现（交易队列和风险监控队列）

- ✅ **系统基础设施**
  - 服务启动和关闭逻辑
  - 信号处理
  - 日志系统

- ⚠️ **交易执行模块** (部分实现)
  - 交易队列处理器
  - 交易机会分析与决策逻辑
  - 交易规模计算
  - 模拟交易执行（未对接实际交易所交易API）
  - 持仓管理基础结构

### 待开发的功能

- ⚠️ **交易执行模块** (待完成部分)
  - 实际交易所交易API对接
  - 合约交易的具体实现
  - 现货对冲交易的具体实现
  - 交易结果处理

- ❌ **风险管理模块**
  - 清算风险监控
  - 对冲偏差监控
  - 风险缓解策略实现
  - 持仓管理与周期性评估

- ❌ **数据分析与优化**
  - 历史资金费率分析
  - 交易表现评估
  - 策略参数优化
  - 盈亏分析报告

- ❌ **通知系统**
  - Telegram通知实现
  - 不同类型通知的处理逻辑
  - 通知频率控制

- ❌ **持久化存储**
  - PostgreSQL集成
  - 交易记录持久化
  - 资金费率历史数据持久化
  - 数据库迁移脚本

- ❌ **用户界面/API**
  - REST API或简单Web界面
  - 系统状态监控
  - 手动操作接口

## 系统要求

- Go 1.18+
- Redis 6.0+
- 交易所API密钥

## 安装

1. 克隆代码库:

```bash
git clone https://github.com/your-username/funding-arb.git
cd funding-arb
```

2. 安装依赖:

```bash
go mod tidy
```

3. 配置系统:

复制并编辑配置文件:

```bash
cp config/config.yaml config/config.local.yaml
```

在`config.local.yaml`中填入你的交易所API密钥和其他配置信息:

```yaml
exchanges:
  binance:
    enabled: true
    api_key: "你的币安API密钥"
    api_secret: "你的币安API密钥"
  # 其他交易所配置...
```

4. 编译:

```bash
go build -o funding-bot cmd/fundingarb/main.go
```

## 使用方法

1. 启动Redis服务:

```bash
redis-server
```

2. 编辑配置文件 `config/config.local.yaml`:

调整参数，如最低年化收益率阈值、最大杠杆倍数、交易对列表等。

3. 运行套利系统:

```bash
./funding-bot -config ./config/config.local.yaml
```

## 配置说明

系统的主要配置项在`config/config.yaml`文件中：

### 交易所配置
```yaml
exchanges:
  binance:
    enabled: true       # 是否启用币安
    api_key: "你的密钥"  # 币安API密钥
    api_secret: "你的密钥" # 币安API密钥
```

### 交易策略配置
```yaml
trading:
  min_yearly_funding_rate: 50.0  # 最低年化收益率阈值(%)
  max_leverage: 5                # 最大杠杆倍数
  max_position_size_percent: 0.6 # 单个头寸最大资金占比
  allowed_symbols:               # 允许交易的交易对列表
    - "BTC/USDT"
    - "ETH/USDT"
```

### 风险管理配置
```yaml
risk_management:
  liquidation_warning_threshold: 0.25  # 清算警告阈值
  hedge_rebalance_threshold: 0.03      # 对冲再平衡阈值
  max_position_time_days: 14           # 最大持仓时间(天)
```

### 系统配置
```yaml
system:
  funding_rate_check_interval_minutes: 10  # 资金费率检查间隔(分钟)
  log_level: "INFO"                        # 日志级别
```

## 系统架构

系统由以下主要模块组成：

1. 交易所接口模块
2. 资金费率监控模块
3. 交易执行模块
4. 风险管理模块
5. 数据存储与分析模块

## 开发

项目使用标准的Go代码结构：

```
.
├── cmd/             # 应用程序入口
├── internal/        # 内部代码
│   ├── config/      # 配置加载
│   ├── exchange/    # 交易所接口
│   ├── monitor/     # 资金费率监控
│   ├── services/    # 主服务
│   ├── storage/     # 数据存储
│   └── trading/     # 交易执行
├── config/          # 配置文件
└── README.md        # 项目说明
```

## 开发贡献指南

1. **代码风格**
   - 遵循Go语言标准代码风格
   - 使用`gofmt`格式化代码
   - 添加适当的注释和文档字符串

2. **提交规范**
   - 使用清晰、简洁的提交信息
   - 每个提交专注于一个特定功能或修复
   - 使用前缀标记提交类型（如`feat:`、`fix:`、`docs:`等）

3. **分支管理**
   - `main`: 稳定分支，包含经过测试的代码
   - `dev`: 开发分支，新功能合并到此分支
   - 功能分支: 基于`dev`创建，命名为`feature/功能名称`

4. **更新项目状态**
   - 功能完成后，更新README.md中的项目状态
   - 将相应功能从"待开发"移至"已完成"
   - 添加任何新发现的功能需求到"待开发"列表

## 重要说明

- 该系统涉及实际资金交易，请谨慎使用
- 先在测试环境验证策略再用于实盘
- 建议从小额资金开始，熟悉系统后再增加投资
- **安全提示**：不要将包含API密钥的配置文件提交到代码仓库中

## 许可证

MIT 
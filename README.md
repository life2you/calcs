# Solana链上自动交易工具 - 数据分析与交易执行模块 (Python)

## 模块职责

`calcs`模块是Solana链上自动交易工具的核心业务逻辑模块，负责数据分析、交易决策和API服务，使用Python实现以快速开发和迭代。主要职责包括：

### 1. 数据分析与交易决策
- 接收并处理`datas`模块提供的链上实时数据
- 应用各种交易策略和技术分析模型分析市场机会
- 基于分析结果生成交易信号和执行决策
- 计算止损和止盈位置，管理风险
- 实现回测系统，评估和优化交易策略

### 2. 交易执行
- 通过调用`datas`模块执行代币交易
- 管理交易分批执行和阶梯式买入/卖出
- 监控订单状态和执行结果
- 处理交易失败的重试机制
- 执行投资组合管理和资金分配

### 3. 市场监控
- 监控热门代币的价格走势和流动性变化
- 识别潜在的早期投资机会
- 分析市场情绪和趋势
- 监控可疑代币活动

### 4. API服务
- 提供RESTful API接口，支持与`views`前端模块交互
- 实现用户配置和偏好设置的管理
- 提供交易历史、持仓和绩效分析数据
- 支持用户手动干预和策略调整
- 提供系统状态和监控数据

## 与其他模块的联动

### 与datas模块的联动 (Rust)
- **数据接收**: 从`datas`模块接收实时的链上数据和市场信息
- **指令发送**: 向`datas`模块发送交易执行指令
- **联动方式**:
  - 通过Redis作为消息队列进行数据传递
  - 通过MongoDB共享持久化数据

### 与views模块的联动 (React)
- **数据展示**: 为前端提供交易数据、市场分析和系统状态
- **用户输入**: 接收并处理用户通过前端设置的交易参数和策略选择
- **联动方式**:
  - 提供RESTful API接口供前端调用
  - WebSocket连接提供实时数据更新
  - JWT认证和安全通信

## 职责界限

为了确保系统各模块高效协作而不重复工作，`calcs`模块作为业务逻辑层有以下明确的职责界限：

### 明确职责范围
- **策略设计与实现**：开发和维护各种交易策略
- **交易信号生成**：基于分析结果产生买入/卖出信号
- **风险管理**：计算止损/止盈点位，资金分配
- **市场分析**：基于`datas`提供的数据进行高级市场分析
- **API服务**：提供RESTful API和WebSocket接口给前端
- **认证与授权**：管理用户登录和权限控制

### 不负责的范围
- ❌ 直接与区块链节点通信（由`datas`模块负责）
- ❌ 原始区块链数据解析（由`datas`模块负责）
- ❌ 交易签名和上链操作（交由`datas`执行）
- ❌ 用户界面渲染和前端交互（由`views`模块负责）

### 接口定义

#### 与datas模块的接口
- **调用datas服务**:
  - `submit_transaction(tx_data)` - 发送交易执行请求
  - `get_token_info(token_address)` - 获取代币详细信息
  - `get_liquidity_data(pool_address)` - 获取流动性池数据

#### 提供给views模块的接口
- **RESTful API**:
  - `/api/auth` - 用户认证
  - `/api/tokens` - 代币数据
  - `/api/trades` - 交易历史和状态
  - `/api/strategies` - 策略配置和管理
  - `/api/dashboard` - 仪表盘统计数据
- **WebSocket接口**:
  - 实时价格和交易状态更新
  - 系统事件和通知推送

## 技术栈

- **核心语言**: Python 3.9+
- **数据处理**: Pandas, NumPy
- **Web框架**: Flask, Flask-CORS
- **数据库**: MongoDB, Redis
- **异步处理**: asyncio
- **定时任务**: schedule
- **日志管理**: loguru

## 文件结构

```
calcs/
├── main.py              # 主程序入口点
├── api.py               # API服务模块
├── monitor.py           # 市场监控模块
├── trading.py           # 交易策略和执行模块
├── requirements.txt     # Python依赖列表
├── start.sh             # 启动脚本
├── .env                 # 环境变量配置
├── logs/                # 日志目录
└── scripts/             # 辅助脚本
```

## 关键配置参数

配置在`.env`文件中设置，主要包括：

```
# 数据库配置
MONGODB_URI=mongodb://username:password@host:port/database
REDIS_URL=redis://localhost:6379/0

# 交易配置
MAX_INVESTMENT_PER_TRADE=0.05  # 单笔交易最大投资比例
STOP_LOSS_PERCENTAGE=10        # 止损百分比
TAKE_PROFIT_PERCENTAGE=30      # 止盈百分比
TOKEN_REFRESH_INTERVAL=60      # 代币刷新间隔(秒)
TRADING_ENABLED=true           # 是否启用交易

# API设置
API_PORT=5000
API_HOST=127.0.0.1
ENABLE_API=true
```

## 启动和使用

```bash
# 启动所有服务
./start.sh all

# 只启动监控服务
./start.sh monitor

# 只启动交易服务
./start.sh trading

# 只启动API服务
./start.sh api
```

## 许可证

MIT License
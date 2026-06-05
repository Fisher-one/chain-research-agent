# Chain Research Agent

> AI × Web3 Hackathon — Cobo 赛道 | A2A Economy × Agent Resource Procurement

两个 AI Agent，各自持有 CAW 钱包，在没有任何人类参与的情况下完成**协商、支付、交付**的完整经济闭环。

- **Orchestrator Agent**（Python）：接收用户任务，判断是否需要采购数据，通过 CAW 自主完成链上支付
- **Data Worker Agent**（Go）：提供付费数据服务，持有独立收款钱包，验证链上付款后交付数据

用户只需说一句话，两个 Agent 之间的资金流转全程自动，链上可查。

---

## A2A 架构

这是 Agent Economy 的最小原型：两个 Agent 各自持有 CAW 钱包，通过 x402 协议完成 Agent-to-Agent 的资源采购和自动结算。

```
用户（自然语言）
    ↓
Orchestrator Agent（Python + CAW 钱包 A）
    │
    │  免费额度内：直接获取数据（慢通道）
    │  超出限速：触发链上支付
    │     ↓ CAW Pact 授权范围内自主签名
    │     ↓ Sepolia 链上转账
    ▼
Data Worker Agent（Go + CAW 钱包 B）
    │  验证 tx_hash → 确认收款
    │  返回真实链上数据（DefiLlama）
    ▼
Orchestrator Agent 分析 → 报告（含 tx_hash 链上记录）
    ↓
用户
```

**为什么必须用 CAW**：Orchestrator 持有的 CAW Pact 限制了它能花多少钱、付给谁、最多付几次。换成普通 EOA 钱包，Agent 就变成「无限授权自动付款」——权限边界消失，这是 Agent 自主支付场景里最核心的安全问题。

---

## 支付触发机制

Data Worker 实现了两级访问控制：

```
Orchestrator 发起请求
    ├─ 免费额度内（< 2次/分钟）→ 200，慢通道（400ms 延迟）
    ├─ 超出限速 → 429，返回收款地址 + 金额
    │       ↓ CAW 自动付款
    │       ↓ 带 X-Payment-Proof: tx_hash 重发
    │       → 200，优先通道（立即响应）
    └─ 付款验证失败 → 402
```

这个设计和真实 API 商业模式一致：**付费买的不是数据本身，而是优先访问权**——就像 Alchemy、Etherscan Pro 的逻辑。

---

## Demo 运行

### 环境要求

- Go 1.21+
- Python 3.11+
- [caw CLI](https://www.cobo.com/products/agentic-wallet/manual/developer/quickstart-overview)（已 onboard 的 Cobo Agentic Wallet）

### 快速开始

```bash
# 1. 克隆项目
git clone https://github.com/Fisher-one/chain-research-agent.git
cd chain-research-agent

# 2. 配置环境变量
cp .env.example .env
# 填写：LLM_API_KEY, LLM_BASE_URL, AGENT_WALLET_ADDRESS

# 3. 启动 x402 数据服务（Go）
cd server && go run main.go &

# 4. 安装 Python 依赖
cd ../agent
python3.11 -m venv .venv && source .venv/bin/activate
pip install cobo-agentic-wallet openai python-dotenv requests

# 5. 运行 Agent
python main.py "查一下过去 7 天 Uniswap 上 USDC/ETH 池的交易量，对比 Curve 同池数据"
```

### 预期输出

```
用户: 查一下过去 7 天 Uniswap 上 USDC/ETH 池的交易量，对比 Curve 同池数据

Agent: 我来查询这两个数据。

[工具调用] fetch_data({'query': 'Uniswap USDC/ETH 7天交易量'})
  📡 请求数据: Uniswap USDC/ETH 7天交易量
  💳 需要付款: 0.001 SETH → 0x74aae83c...
  📋 提交 Pact...
  ✅ Pact 激活: 950eb285...
  💸 发起转账...
  ✅ 转账成功: 0xc8bb1c4026...
  🔄 重发请求（带付款证明）...

Agent: 📊 Uniswap vs Curve USDC/ETH 池 7 天交易量对比
  Uniswap: 1,234,567 USDC (~73%)
  Curve:   456,789  USDC (~27%)
  支付记录: 0.002 SETH 总费用，tx hash 链上可查
```

---

## 项目架构

```
chain-research-agent/
├── server/
│   └── main.go    # Data Worker Agent（Go）
│                  #   持有收款钱包 0x74aae83c...（Sepolia）
│                  #   GET /data → 免费慢通道 / 429限速 / 200优先通道
│                  #   GET /health → 健康检查
├── agent/
│   ├── main.py    # Orchestrator Agent（Python + LLM）
│                  #   持有 CAW 钱包，受 Pact 权限约束
│   ├── tools.py   # fetch_data() — 封装 x402 + CAW 支付逻辑
│   └── spike_caw.py  # CAW 独立验证脚本
└── .env.example
```

### 两个 Agent 的钱包

| 角色 | 技术 | 钱包地址 | 作用 |
|------|------|---------|------|
| Orchestrator Agent | Python + CAW SDK | 由 `caw` CLI 管理 | 花钱方，受 Pact 约束 |
| Data Worker Agent | Go HTTP Server | `0x74aae83c8bf22c72a9246b33fc793f20af79e64b` | 收钱方，验证 tx_hash 后交付数据 |

---

## CAW 集成说明

### Pact 权限配置

每次数据查询前，`fetch_data()` 自动提交一个 Pact：

```python
policies = [{
    "name": "x402-payment",
    "type": "transfer",
    "rules": {
        "effect": "allow",
        "when": {
            "chain_in": ["SETH"],
            "token_in": [{"chain_id": "SETH", "token_id": "SETH"}]
        },
        "deny_if": {"amount_gt": "0.002"}  # 上限是要求金额的 2 倍
    }
}]
completion_conditions = [{"type": "tx_count", "threshold": "5"}]
```

Pact 激活后，Agent 在授权范围内自主完成转账，超出范围的操作自动被拒绝。

### Agent Wallet 地址

```
0x74aae83c8bf22c72a9246b33fc793f20af79e64b  (Sepolia)
```

### 链上交易记录（已验证）

| 查询 | tx hash | 状态 |
|------|---------|------|
| CAW spike 验证 | [0xe77dcf36...](https://sepolia.etherscan.io/tx/0xe77dcf36b81eeee1eb016b6c5dd53419c2bbdc3bd34e584dc03deeea0fafb2cc) | ✅ Success |
| Uniswap 数据查询 | [0xc8bb1c40...](https://sepolia.etherscan.io/tx/0xc8bb1c4026) | ✅ Success |
| Curve 数据查询 | [0x39326d60...](https://sepolia.etherscan.io/tx/0x39326d6029) | ✅ Success |

---

## x402 协议说明

x402 把「付款触发」嵌入 HTTP 协议本身：

```
Client → GET /data
Server ← 402 Payment Required + {address, amount, token}
Client → CAW 自主付款 → 拿到 tx_hash
Client → GET /data (X-Payment-Proof: tx_hash)
Server → 验证 tx_hash → 200 OK + 数据
```

Agent 通过 `fetch_data()` 工具调用这个流程，对 LLM 完全透明——LLM 只看到「查询成功，返回了数据」。

---

## 风险边界

| 风险 | 控制方式 |
|------|---------|
| Agent 超额支付 | Pact `deny_if.amount_gt` 限制单笔上限 |
| 重复支付 | `request_id` 幂等键防止重复提交 |
| 转账到错误地址 | x402 server 地址在代码里写死，Pact policy 限制 token 类型 |
| Agent 失控 | Pact `tx_count` 限制最大次数，到期自动失效 |

---

## 技术栈

| 组件 | 技术 |
|------|------|
| Agent LLM | DeepSeek Chat（OpenAI 兼容） |
| 支付层 | Cobo CAW（cobo-agentic-wallet SDK + caw CLI） |
| 支付协议 | x402（HTTP 402 机制） |
| 数据服务 | Go net/http |
| 测试网 | Ethereum Sepolia |

---

## 当前限制与下一步

**当前**：
- 数据来自 DefiLlama 公开 API（真实数据），Data Worker 作为付费访问代理
- x402 验证在 Etherscan 限速时宽松通过（dev mode）
- 支付 token 为 SETH（Sepolia 测试网原生代币）

**下一步**：
- 多轮研究闭环：Orchestrator 自主决策多次采购、交叉验证后出报告
- 多个 Data Worker：不同数据源、不同定价，Orchestrator 根据预算自动选择
- x402 验证升级为 Etherscan Pro API（提高速率限制）
- 支持 USDC 作为支付 token

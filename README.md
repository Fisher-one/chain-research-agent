# Chain Research Agent

> AI × Web3 Hackathon — Cobo 赛道 | Agent-Native Payments + Agent Resource Procurement

用自然语言委托 AI Agent 完成链上数据调研。Agent 在 Cobo CAW Pact 授权范围内，通过 x402 协议**自主购买付费数据**，完成分析后返回报告。全程无需用户手动签名。

---

## 核心流程

```
用户一句话
    ↓
Agent（LLM）理解意图 → 调用 fetch_data() 工具
    ↓
fetch_data() 请求 x402 数据服务 → 收到 402 + 付款要求
    ↓
CAW 在 Pact 授权范围内自主签名 → Sepolia 链上转账
    ↓
带 tx_hash 重发请求 → 服务验证付款 → 返回数据
    ↓
Agent 分析数据 → 生成报告（含链上支付记录）
```

**CAW 是不可替换的核心组件**：Agent 的每一笔数据购买都通过 CAW 完成，Pact 机制限制了授权范围（金额上限、token 类型、次数），确保 Agent 只能在预设边界内自主支付。

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
│   └── main.go          # Go — x402 Paywall Server
│                        #   GET /data  → 402 或返回数据
│                        #   GET /health → 健康检查
├── agent/
│   ├── main.py          # Python — LLM Agent（tool calling 循环）
│   ├── tools.py         # fetch_data() 工具（封装 x402 + CAW 逻辑）
│   └── spike_caw.py     # CAW 独立测试脚本
└── .env.example
```

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

**当前**：数据服务返回 mock 数据，x402 验证在 Etherscan 限速时宽松通过。

**下一步**：
- 接入真实 Dune Analytics API（用 x402 付费查询）
- x402 验证升级为 Etherscan Pro API（提高速率限制）
- 支持 USDC 作为支付 token（当前用 SETH 测试）

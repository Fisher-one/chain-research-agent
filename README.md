# Chain Research Agent

> AI × Web3 Hackathon — Cobo 赛道 | A2A Economy × Agent Resource Procurement

一个 Orchestrator Agent 动态发现多个 Data Worker，比价后通过 CAW（Cobo Agentic Wallet）完成链上支付，取回真实链上数据，生成结构化报告——全程无需人类参与任何链上操作。

**Agent Wallet（Sepolia）：** `0x74aae83c8bf22c72a9246b33fc793f20af79e64b`
**链上 tx：** [0xe77dcf36...](https://sepolia.etherscan.io/tx/0xe77dcf36b81eeee1eb016b6c5dd53419c2bbdc3bd34e584dc03deeea0fafb2cc)

---

## ⚠️ Narrative Warning

**Go server 不是普通 API，是具有独立经济身份的自主服务。**

区别在哪里：
- 它持有独立的 Sepolia 收款钱包（不是转账给 Stripe 或某个中心化服务）
- 它自主验证链上付款（不依赖任何中间人）
- 它可以拒绝未付款的请求、选择不服务（经济主权）
- 它的「身份证明」是链上可查的收款记录，不是 API Key

和调用 Stripe API 的本质差异：Stripe 是中心化中间人，这里的 Worker 本身就是经济参与者。

---

## A2A 架构

```
用户（自然语言）
      │
      ▼
Orchestrator Agent（Python + CAW 钱包）
      │
      ├── list_data_workers() ──→ GET /catalog × 3（HTTP 探测）
      │         │
      │    ┌────┴──────────────────────────────────────┐
      │    ▼                  ▼                         ▼
      │ Worker :8081       Worker :8082            Worker :8083
      │ DefiLlama TVL      DefiLlama Yields        CoinGecko
      │ 0.001 SETH         0.0015 SETH             0.002 SETH
      │    └────┬──────────────────────────────────────┘
      │         │ Worker 列表（含定价、专长）
      │         ▼
      │    LLM 推理比价，选最优 Worker 组合
      │
      ├── hire_worker() → 发起数据请求
      │         │
      │         │ 429（超出免费额度）
      │         ▼
      │    CAW Pact 检查
      │    ├── 地址在白名单？
      │    ├── 金额 ≤ 上限？
      │    └── 未超交易次数？
      │         │ 通过
      │         ▼
      │    Sepolia 链上转账（自主签名）
      │         │ X-Payment-Proof: tx_hash
      │         ▼
      │    Worker 验证链上收款 → 返回真实数据
      │
      ▼
生成结构化分析报告（含支付摘要页脚）
      │
      ▼
    用户
```

**为什么 CAW 比 EOA 更适合 Agent**：EOA 给 Agent 就是全权限——prompt injection 攻击可以让 Agent 转账给任意地址。CAW Pact 的约束在合约层，绕过代码也无法突破。

---

## 完整 A2A 经济闭环

```
发现  ✅  list_data_workers() → GET /catalog（真实 HTTP 探测，无 mock）
比价  ✅  LLM 自己推理选哪个 Worker（过程透明）
采购  ✅  hire_worker() → CAW Pact + Sepolia 链上支付
交付  ✅  x402 验证 + 真实 DefiLlama / CoinGecko 数据
审计  ✅  tx hash 附在报告页脚，链上可查
```

---

## 支付触发机制

Data Worker 实现两级访问控制：

```
Orchestrator 发起请求
    ├─ 免费额度内（< 2次/分钟）→ 200，慢通道（400ms 延迟）
    ├─ 超出限速 → 429，返回 { payment_address, amount, token_id }
    │       ↓ CAW 在 Pact 范围内自主签名
    │       ↓ Sepolia 链上转账
    │       ↓ 重发 + X-Payment-Proof: tx_hash
    │       → 200，优先通道（立即响应）
    └─ 付款验证失败 → 402
```

这和真实 API 商业模式一致：**付的是优先访问权**，不是数据本身——和 Alchemy、Etherscan Pro 的逻辑相同。

---

## 快速开始

### 环境要求

- Go 1.21+
- Python 3.11+
- [`caw` CLI](https://www.cobo.com/products/agentic-wallet/manual/developer/quickstart-overview)（已 onboard 的 Cobo Agentic Wallet）

### 运行步骤

```bash
# 1. 克隆
git clone https://github.com/Fisher-one/chain-research-agent.git
cd chain-research-agent

# 2. 配置环境变量
cp .env.example .env
# 填写：LLM_API_KEY, LLM_BASE_URL, AGENT_WALLET_ADDRESS

# 3. 安装 Python 依赖
cd agent
python3.11 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt

# 4. 启动三个 Data Worker（新终端）
cd ..
./start-workers.sh
# 输出：
#   ✅ Worker 8081 | DefiLlama Protocols  | 0.001 SETH  | TVL & DEX 交易量
#   ✅ Worker 8082 | DefiLlama Yields     | 0.0015 SETH | 收益率 & APY
#   ✅ Worker 8083 | CoinGecko Market     | 0.002 SETH  | 代币价格 & 市值

# 5. 运行 Agent（第四个终端）
cd agent && source .venv/bin/activate
python3.11 main.py
```

### 示例输出

```
用户: 分析 Ethereum DeFi 生态的 TVL、收益率和主要代币价格

  🔍 探测 3 个已知 Worker 地址...
  📡 发现 3 个在线 Worker:
     · DefiLlama Protocols Worker (0.001 SETH) — DeFi 协议 TVL 与 DEX 交易量
     · DefiLlama Yields Worker (0.0015 SETH) — DeFi 收益率与 APY 数据
     · CoinGecko Market Worker (0.002 SETH) — 代币价格、市值与 24h 涨跌幅

  🤝 雇用 DefiLlama Protocols Worker (0.001 SETH)
  🚦 免费额度用完，升级优先通道: 0.001 SETH → 0x74aae83c...
  📋 提交 Pact...
  ✅ Pact 激活: 950eb285...
  💸 链上转账...
  ✅ 支付成功: 0xe77dcf36...
  🔄 取回数据...

Agent: ## Ethereum DeFi 生态分析报告

### TVL 分析（来源：DefiLlama）
Lido：$32.4B TVL，以太坊最大质押协议...

### 收益率（来源：DefiLlama Yields）
AAVE USDC：4.2% APY | Compound ETH：2.8% APY...

### 代币价格（来源：CoinGecko）
ETH：$3,421 (+2.1% 24h) | BTC：$67,800...

---
💳 支付摘要
Worker 8081 (DefiLlama TVL)：0x74aae8... → 0.001 SETH | tx: 0xe77dcf36...
Worker 8082 (DefiLlama Yields)：0x74aae8... → 0.0015 SETH | tx: 0xabcd1234...
Worker 8083 (CoinGecko)：0x74aae8... → 0.002 SETH | tx: 0xefgh5678...
总计：0.0045 SETH | 全部链上可查
```

---

## 项目结构

```
chain-research-agent/
├── agent/
│   ├── main.py         # Orchestrator Agent（LLM 推理 + 工具调用）
│   ├── registry.py     # Worker 服务发现（HTTP 探测 /catalog，无静态字典）
│   ├── tools.py        # list_data_workers() + hire_worker() + fetch_data()
│   ├── spike_caw.py    # CAW 独立验证脚本
│   └── requirements.txt
├── server/
│   └── main.go         # Data Worker（单文件，WORKER_TYPE 环境变量切换身份）
│                       #   GET /catalog  → Worker 自我描述（名称、价格、专长）
│                       #   GET /data     → 免费慢通道 / 429限速 / 验证后优先通道
│                       #   GET /health   → 健康检查
├── start-workers.sh    # 一键启动三个 Worker（端口 8081 8082 8083）
└── .env.example
```

---

## CAW 集成说明

### Pact 权限配置

每次触发付款时，`hire_worker()` 自动提交 Pact：

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
        "deny_if": {"amount_gt": str(float(amount) * 2)}  # 上限 = 要求金额 × 2
    }
}]
completion_conditions = [{"type": "tx_count", "threshold": "5"}]
```

Pact 激活后，CAW 在授权范围内自主签名转账；超出范围（地址不在白名单、超金额）自动拒绝。

### 链上记录

| 交易 | tx hash | 网络 |
|------|---------|------|
| CAW spike 验证 | [0xe77dcf36...](https://sepolia.etherscan.io/tx/0xe77dcf36b81eeee1eb016b6c5dd53419c2bbdc3bd34e584dc03deeea0fafb2cc) | Sepolia ✅ |
| demo_guard 场景 1（正常支付） | [0x77412c3d...](https://sepolia.etherscan.io/tx/0x77412c3d1da75edd398a59d617b5c867805b2a9d771f6220a549e59bf174ca09) | Sepolia ✅ |

---

## 安全边界演示（被阻止的攻击）

```bash
# 在 Agent 目录里运行（需要先 ./start-workers.sh）
cd agent && source .venv/bin/activate
python demo_guard.py
```

演示三个场景：

| 场景 | 攻击方式 | 结果 |
|------|---------|------|
| 正常支付 | — | ✅ 成功，链上可查 |
| 超额攻击 | Prompt injection 诱导 Agent 支付 1.0 SETH | 🚫 Pact 超限拒绝 |
| 地址替换 | 攻击者把收款地址换成自己的钱包 | 🚫 Pact 白名单拒绝 |

**关键结论**：CAW Pact 的约束在合约层执行，不在 Agent 代码里。即使攻击者通过 prompt injection 完全控制了 Agent 的决策逻辑，Pact 仍在最终执行层拦截越权操作。这是信任边界在合约层而非代码层的直接证明。

---

## 已知漏洞

这两个漏洞已知、有一阶防线，但没有根本修法（超出 MVP 范围）：

### 合法浪费攻击（Legitimate Waste Attack）

攻击者操控用户输入，让 Agent 反复查询无用数据。每次支付都在 Pact 范围内，但累积耗尽预算。

**当前防线：** rate limit（2次/分钟免费）+ Pact `tx_count` 限制
**根本修法：** session token 绑定请求方身份，让系统区分「正常查询」和「恶意刷量」

### Sybil Attack

攻击者注册大量 Worker，刷低价（0.0001 SETH）吸引 Orchestrator 选择，但返回垃圾数据或拒绝交付。

**当前防线：** 无（MVP 接受此风险）
**根本修法：** Worker reputation layer，需要历史记录 + 质押机制

---

## A2A 路线图

当前实现是「受控的经济基元」——Agent 在预算边界内采购资源。下一步扩展方向：

| 阶段 | 功能 | 状态 |
|------|------|------|
| MVP | 发现 → 比价 → 支付 → 交付 | ✅ 已完成 |
| 下一步 | Pact 动态白名单（基于链上 Worker Registry） | 🔲 规划中 |
| 下一步 | Escrow 合约（防止收款不交付） | 🔲 规划中 |
| 下一步 | Worker reputation layer（防 Sybil） | 🔲 规划中 |
| 长期 | 多 Agent 协作（Orchestrator 也可以是 Worker） | 🔲 规划中 |

---

## 技术栈

| 组件 | 技术 |
|------|------|
| Orchestrator Agent | Python + LLM（OpenAI 兼容接口） |
| 支付层 | Cobo Agentic Wallet（`cobo-agentic-wallet` SDK + `caw` CLI） |
| 支付触发协议 | x402（HTTP 429/402 机制） |
| Data Worker | Go（单文件，多实例） |
| 数据来源 | DefiLlama API + CoinGecko API（真实数据） |
| 测试网 | Ethereum Sepolia |

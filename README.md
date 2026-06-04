# Chain Research Agent

> AI × Web3 Hackathon — Cobo 赛道 | Agent Resource Procurement

用自然语言委托 Agent 完成链上数据调研。Agent 在 Cobo CAW Pact 授权范围内，通过 x402 协议自主购买付费数据，完成分析后返回报告。全程无需用户手动签名。

## 项目结构

```
chain-research-agent/
├── server/          # Go — x402 Paywall Server
│   └── main.go
├── agent/           # Python — LLM Agent + CAW 支付层
│   ├── main.py      # Agent 入口
│   ├── tools.py     # fetch_data() 工具（含 x402 + CAW 逻辑）
│   ├── spike_caw.py # CAW SDK 验证脚本（开发用）
│   └── requirements.txt
├── .env.example
└── README.md
```

## 核心流程

```
用户输入 → Agent 分析需求 → 调用 fetch_data()
  → 请求 x402 服务 → 收到 402 → CAW 自动支付（Pact 范围内）
  → 带 tx hash 重发 → 拿到数据 → 生成报告 → 输出给用户
```

## 快速开始

```bash
# 1. 配置环境变量
cp .env.example .env
# 填写 COBO_API_KEY, LLM_API_KEY 等

# 2. 启动 x402 服务（Go）
cd server && go run main.go

# 3. 运行 Agent（Python）
cd agent && pip install -r requirements.txt
python main.py
```

## CAW 权限配置（Pact）

| 参数 | 值 |
|------|-----|
| 单笔上限 | 10 USDC |
| 日限额 | 50 USDC |
| 白名单地址 | x402 服务收款地址（Sepolia） |
| 有效期 | 30 天 |

## 技术栈

- **Agent**: Python + DeepSeek/Claude API
- **支付层**: Cobo CAW (cobo-waas2 SDK)
- **x402 Server**: Go (net/http)
- **测试网**: Sepolia + USDC

## 开发状态

- [ ] CAW SDK spike（发一笔测试转账，拿到 tx hash）
- [ ] Go x402 server（返回 402 + 验证付款证明）
- [ ] fetch_data() 工具（x402 + CAW 循环）
- [ ] Agent loop（tool calling）
- [ ] 端到端联调

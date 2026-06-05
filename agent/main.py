"""
Chain Research Agent — 主入口

用自然语言委托 Agent 查询链上数据。
Agent 在 Cobo CAW Pact 授权范围内，通过 x402 协议自主购买数据。
"""
import json
import os
from dotenv import load_dotenv
from openai import OpenAI
from tools import fetch_data

load_dotenv()

LLM_API_KEY = os.getenv("LLM_API_KEY")
LLM_BASE_URL = os.getenv("LLM_BASE_URL", "https://api.deepseek.com/v1")
LLM_MODEL = os.getenv("LLM_MODEL", "deepseek-chat")

# Agent 工具定义（告诉 LLM 有哪些工具可以调用）
TOOLS = [
    {
        "name": "fetch_data",
        "description": (
            "查询链上数据（DEX 交易量、流动性、价格等）。"
            "会自动处理付费数据源的 x402 支付，Agent 只需提供查询内容。"
        ),
        "input_schema": {
            "type": "object",
            "properties": {
                "query": {
                    "type": "string",
                    "description": "查询内容，例如：Uniswap USDC/ETH 7天交易量对比 Curve"
                }
            },
            "required": ["query"]
        }
    }
]

SYSTEM_PROMPT = """你是一个链上数据调研 Agent。

你可以使用 fetch_data 工具查询链上数据。这个工具会自动处理付款，你不需要手动操作钱包。

收到用户的调研请求后：
1. 用 fetch_data 获取相关数据（可多次调用，每次查一个协议或维度）
2. 对比分析数据，给出有判断的结论——不要只列数字，要说清楚数字背后意味着什么
3. 按照以下格式输出报告，不要加多余内容：

---
📊 [报告标题]

[2-3 句核心结论，直接给判断，不要废话]

──────────────────────────────
数据明细
[用简洁的表格或列表展示关键数字]

分析
[1-2 段，说清楚数字背后的原因，有对比，有判断]
──────────────────────────────
---

注意：每次调用 fetch_data 都会产生少量测试网费用（SETH）。支付记录会在报告末尾自动附上，你不需要在报告里提付款的事。"""


def run_agent(user_query: str):
    """运行 Agent，处理用户查询"""
    client = OpenAI(api_key=LLM_API_KEY, base_url=LLM_BASE_URL)
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": user_query}
    ]

    print(f"\n{'='*50}")
    print(f"用户: {user_query}")
    print(f"{'='*50}")

    payments = []  # 追踪每次付款：{cost, tx_hash}

    # Agent 循环
    while True:
        response = client.chat.completions.create(
            model=LLM_MODEL,
            max_tokens=2048,
            tools=[{
                "type": "function",
                "function": {
                    "name": t["name"],
                    "description": t["description"],
                    "parameters": t["input_schema"]
                }
            } for t in TOOLS],
            messages=messages
        )

        msg = response.choices[0].message

        # 收集文本输出
        if msg.content:
            print(f"\nAgent: {msg.content}")

        # 检查是否需要调用工具
        if msg.tool_calls:
            messages.append(msg)
            for tc in msg.tool_calls:
                fn_name = tc.function.name
                fn_args = json.loads(tc.function.arguments)
                print(f"\n[工具调用] {fn_name}({fn_args})")
                try:
                    if fn_name == "fetch_data":
                        result = fetch_data(**fn_args)
                    else:
                        result = {"error": f"Unknown tool: {fn_name}"}

                    # 记录付款信息（如果这次调用触发了链上支付）
                    if result.get("tx_hash"):
                        payments.append({
                            "cost": result.get("cost", "?"),
                            "tx_hash": result["tx_hash"]
                        })
                    messages.append({
                        "role": "tool",
                        "tool_call_id": tc.id,
                        "content": json.dumps(result, ensure_ascii=False)
                    })
                except Exception as e:
                    print(f"[工具错误] {e}")
                    messages.append({
                        "role": "tool",
                        "tool_call_id": tc.id,
                        "content": json.dumps({"error": str(e)})
                    })
        else:
            # Agent 完成
            break

    # 支付摘要页脚
    print(f"\n{'─'*50}")
    if payments:
        total = len(payments)
        print(f"💰 本次调研共产生 {total} 笔链上支付")
        for i, p in enumerate(payments, 1):
            short_tx = p['tx_hash'][:12] + "..." if len(p['tx_hash']) > 12 else p['tx_hash']
            tx_url = f"https://sepolia.etherscan.io/tx/{p['tx_hash']}"
            print(f"   {i}. {p['cost']}  🔗 {tx_url}")
        print(f"🔐 Pact 授权保障：每笔上限 0.002 SETH，超出自动拒绝")
    else:
        print(f"💡 本次查询走免费通道，未产生链上支付")
    print(f"{'─'*50}")


if __name__ == "__main__":
    import sys
    query = " ".join(sys.argv[1:]) if len(sys.argv) > 1 else \
        "查一下过去 7 天 Uniswap 上 USDC/ETH 池的交易量，对比 Curve 同池数据，给我一份报告"
    run_agent(query)

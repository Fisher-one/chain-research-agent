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
1. 用 fetch_data 获取相关数据
2. 分析数据，生成简洁的分析报告
3. 报告包括：关键数据、对比结论、数据来源和支付记录

注意：每次调用 fetch_data 都会产生少量测试网费用（SETH）。"""


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

                    print(f"[工具结果] {json.dumps(result, ensure_ascii=False)[:200]}...")
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

    print(f"\n{'='*50}")


if __name__ == "__main__":
    import sys
    query = " ".join(sys.argv[1:]) if len(sys.argv) > 1 else \
        "查一下过去 7 天 Uniswap 上 USDC/ETH 池的交易量，对比 Curve 同池数据，给我一份报告"
    run_agent(query)

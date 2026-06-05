"""
fetch_data() — x402 支付工具

Agent 调用这个函数时，内部自动处理：
  1. 请求 x402 server → 收到 402
  2. 用 CAW 提交 Pact + 发转账 → 拿到 tx_hash
  3. 带 X-Payment-Proof 重发请求 → 拿到数据
Agent 只看到数据返回，不需要知道 402 存在。
"""
import json
import os
import subprocess
import time
import requests
from typing import Any

X402_SERVER_URL = os.getenv("X402_SERVER_URL", "http://localhost:8080")

def _get_wallet_address() -> str:
    """从 CAW 获取钱包地址（优先用环境变量）"""
    addr = os.getenv("AGENT_WALLET_ADDRESS", "")
    if addr:
        return addr
    # fallback：从 caw 读取
    result = _caw(["wallet", "current"])
    addresses = result.get("default_addresses", [])
    for a in addresses:
        if a.get("chain_identifier") == "ETH":
            return a["address"]
    raise RuntimeError("Cannot find ETH wallet address")

AGENT_WALLET_ADDRESS = ""  # 延迟初始化，调用时再获取


def _caw(args: list) -> dict:
    """调用 caw CLI，返回解析后的 JSON"""
    result = subprocess.run(
        ["caw"] + args,
        capture_output=True, text=True
    )
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError:
        return {"raw": result.stdout, "stderr": result.stderr}


def _submit_pact(amount: str, token_id: str, dst_address: str) -> str:
    """提交 Pact，返回 pact_id"""
    policies = json.dumps([{
        "name": "x402-payment",
        "type": "transfer",
        "rules": {
            "effect": "allow",
            "when": {
                "chain_in": ["SETH"],
                "token_in": [{"chain_id": "SETH", "token_id": token_id}]
            },
            "deny_if": {"amount_gt": str(float(amount) * 2)}  # 上限是要求金额的 2 倍
        }
    }])
    conditions = json.dumps([{"type": "tx_count", "threshold": "5"}])

    result = _caw([
        "pact", "submit",
        "--intent", f"x402 payment: {amount} {token_id} for data access",
        "--execution-plan", f"Transfer {amount} {token_id} to {dst_address}",
        "--policies", policies,
        "--completion-conditions", conditions,
    ])

    pact_id = (result.get("result") or result).get("pact_id")
    if not pact_id:
        raise RuntimeError(f"Failed to get pact_id: {result}")

    # 等待 Pact 激活
    for _ in range(10):
        pact = _caw(["pact", "show", "--pact-id", pact_id])
        status = (pact.get("result") or pact).get("status", "unknown")
        if status == "active":
            return pact_id
        if status in ("rejected", "expired"):
            raise RuntimeError(f"Pact {status}")
        time.sleep(2)

    raise RuntimeError("Pact activation timeout")


def _pay(pact_id: str, amount: str, token_id: str, dst_address: str) -> str:
    """发起转账，返回 tx_hash"""
    import uuid
    request_id = f"x402-{uuid.uuid4().hex[:8]}"
    src_address = _get_wallet_address()
    print(f"  [pay] src={src_address[:10]}... pact={pact_id[:8]}...")

    result = _caw([
        "tx", "transfer",
        "--token-id", token_id,
        "--src-address", src_address,
        "--dst-address", dst_address,
        "--amount", amount,
        "--chain-id", "SETH",
        "--request-id", request_id,
        "--pact-id", pact_id,
    ])

    print(f"  [transfer submit] {json.dumps(result)[:200]}")

    # 如果提交本身失败，提前报错
    if result.get("error") or result.get("stderr"):
        raise RuntimeError(f"Transfer submit failed: {result.get('stderr') or result.get('message') or result}")

    # 等待 tx 完成（最多 3 分钟）
    for i in range(60):
        tx = _caw(["tx", "get", "--request-id", request_id])
        status = tx.get("status", "")
        if status == "Success":
            return tx["transaction_hash"]
        if status in ("Failed", "Rejected"):
            raise RuntimeError(f"Transfer {status}: {tx}")
        if i % 5 == 0:
            print(f"  ⏳ 等待链上确认... ({i*3}s) status={status or 'pending'}")
        time.sleep(3)

    raise RuntimeError("Transfer timeout (3min)")


def fetch_data(query: str) -> dict[str, Any]:
    """
    查询链上数据。内部自动处理 x402 付款。

    Args:
        query: 查询内容，例如 "Uniswap USDC/ETH 7天交易量"

    Returns:
        dict: 数据结果，包含 data、tx_hash、cost 字段
    """
    url = f"{X402_SERVER_URL}/data"
    params = {"q": query}

    print(f"  📡 请求数据: {query}")

    # Step 1: 发起请求
    resp = requests.get(url, params=params)

    if resp.status_code == 200:
        # 直接成功（免费额度内）
        return resp.json()

    if resp.status_code not in (402, 429):
        raise RuntimeError(f"Unexpected status {resp.status_code}: {resp.text}")

    # Step 2: 收到 402（付费墙）或 429（限速），解析付款要求
    # 两种情况的响应体结构一样，都包含 payment_address / amount / token_id
    payment_req = resp.json()
    amount = payment_req["amount"]
    token_id = payment_req["token_id"]
    dst_address = payment_req["payment_address"]

    if resp.status_code == 429:
        print(f"  🚦 免费额度用完，升级到优先通道: {amount} {token_id} → {dst_address[:10]}...")
    else:
        print(f"  💳 需要付款: {amount} {token_id} → {dst_address[:10]}...")

    # Step 3: 提交 Pact
    print(f"  📋 提交 Pact...")
    pact_id = _submit_pact(amount, token_id, dst_address)
    print(f"  ✅ Pact 激活: {pact_id[:8]}...")

    # Step 4: 发起转账
    print(f"  💸 发起转账...")
    tx_hash = _pay(pact_id, amount, token_id, dst_address)
    print(f"  ✅ 转账成功: {tx_hash[:12]}...")

    # Step 5: 带付款证明重发请求
    print(f"  🔄 重发请求（带付款证明）...")
    resp2 = requests.get(url, params=params, headers={"X-Payment-Proof": tx_hash})

    if resp2.status_code != 200:
        raise RuntimeError(f"Data request failed after payment: {resp2.status_code} {resp2.text}")

    result = resp2.json()
    result["tx_hash"] = tx_hash
    result["cost"] = f"{amount} {token_id}"
    return result

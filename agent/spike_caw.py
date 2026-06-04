"""
CAW Spike — 通过 caw CLI 发一笔测试网转账，拿到 tx hash
不依赖 Python SDK，直接调用已安装的 caw 命令
"""
import subprocess
import json
import time
import os
from dotenv import load_dotenv

load_dotenv()

WALLET_ID  = os.getenv("COBO_WALLET_ID", "01a0c6ea-e129-4bb3-8cbe-28659cad3897")
TO_ADDRESS = "0x74aae83c8bf22c72a9246b33fc793f20af79e64b"  # 转给自己

def caw(args: list) -> dict:
    """调用 caw CLI，返回解析后的 JSON"""
    result = subprocess.run(
        ["caw"] + args,
        capture_output=True, text=True
    )
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError:
        return {"raw": result.stdout, "error": result.stderr}

def spike():
    print("🔍 检查钱包状态...")
    status = caw(["wallet", "balance", "--chain-id", "SETH"])
    if status.get("success"):
        balances = status.get("result", [])
        for b in balances:
            print(f"   {b['token_id']}: {b['amount']}")
    else:
        print(f"   ⚠️  {status}")

    print("\n📋 提交 Pact...")
    policies = json.dumps([{
        "name": "spike-transfer",
        "type": "transfer",
        "rules": {
            "effect": "allow",
            "when": {
                "chain_in": ["SETH"],
                "token_in": [{"chain_id": "SETH", "token_id": "SETH"}]
            },
            "deny_if": {"amount_gt": "0.005"}
        }
    }])
    conditions = json.dumps([{"type": "tx_count", "threshold": "1"}])

    pact_result = caw([
        "pact", "submit",
        "--intent", "Spike: transfer 0.001 SETH to self on Sepolia",
        "--execution-plan", "Transfer 0.001 SETH to self for testing",
        "--policies", policies,
        "--completion-conditions", conditions,
    ])

    print(f"   結果: {json.dumps(pact_result, indent=2)[:300]}")

    pact_id = pact_result.get("result", {}).get("pact_id") or pact_result.get("pact_id")
    if not pact_id:
        print("❌ 获取 pact_id 失败")
        return

    print(f"\n⏳ 等待 Pact 激活 (id={pact_id})...")
    for i in range(20):
        pact = caw(["pact", "show", "--pact-id", pact_id])
        status_val = (pact.get("result") or pact).get("status", "unknown")
        print(f"   [{i+1}] status: {status_val}")
        if status_val == "active":
            break
        if status_val in ("rejected", "expired"):
            print(f"❌ Pact {status_val}")
            return
        time.sleep(5)
    else:
        print("❌ Timeout")
        return

    print("\n💸 发送转账...")
    tx_result = caw([
        "tx", "transfer",
        "--token-id", "SETH",
        "--source-address", TO_ADDRESS,
        "--destination-address", TO_ADDRESS,
        "--amount", "0.001",
        "--chain-id", "SETH",
        "--request-id", "spike-001",
        "--pact-id", pact_id,
    ])

    print(f"\n✅ 转账结果:")
    print(json.dumps(tx_result, indent=2)[:500])
    print(f"\n🔍 查看地址:")
    print(f"   https://sepolia.etherscan.io/address/{TO_ADDRESS}")

if __name__ == "__main__":
    spike()

"""
CAW Spike — 验证 Cobo SDK 能否在测试网发出一笔转账
目标：用 CAW SDK 在 Sepolia 发 0.01 USDC 到自己的另一个地址，拿到 tx hash
通过 = 后续 3 天 CAW 接入有底气
不通 = 切换降级方案（ethers.js）
"""
import os
from dotenv import load_dotenv

load_dotenv()

# TODO: 根据 CAW 文档填入正确的初始化方式
# 参考：https://www.cobo.com/products/agentic-wallet/manual/developer/quickstart-overview

def spike_transfer():
    """
    最小测试：发一笔 0.01 USDC 转账到自己的另一个地址
    成功条件：拿到 tx_hash 且在 Sepolia Etherscan 上可查
    """
    api_key = os.getenv("COBO_API_KEY")
    api_secret = os.getenv("COBO_API_SECRET")

    if not api_key or not api_secret:
        print("❌ 请先在 .env 里填写 COBO_API_KEY 和 COBO_API_SECRET")
        return

    print("🔑 CAW credentials loaded")
    print("📡 Connecting to Cobo WaaS2...")

    # TODO: 初始化 CAW client
    # from cobo_waas2 import ...

    # TODO: 发送转账
    # tx = client.transfer(...)

    # TODO: 打印 tx hash
    # print(f"✅ tx hash: {tx.hash}")

    print("⚠️  Spike 代码待填写，参考 CAW 文档完成初始化")

if __name__ == "__main__":
    spike_transfer()

"""
Data Worker Registry — 可用数据服务的注册表

每个 Worker 是一个独立的 x402 数据服务，有自己的：
- 服务地址（URL）
- 专长领域（specialty）
- 价格（price）
- 延迟特性（latency）

Agent 通过 list_data_workers() 发现这些 Worker，
再根据任务需求和预算选择最合适的，调用 hire_worker() 采购。
"""

DATA_WORKERS = {
    "worker_defillama": {
        "url": "http://localhost:8081",
        "name": "DefiLlama Worker",
        "specialty": "DeFi 协议 TVL、DEX 交易量、流动性数据",
        "keywords": ["tvl", "volume", "交易量", "流动性", "dex", "uniswap", "curve", "defi"],
        "price": "0.001 SETH",
        "latency": "fast",
        "description": "适合查主流 DEX 协议的 TVL 和交易量，数据来自 DefiLlama，更新及时",
    },
    "worker_onchain": {
        "url": "http://localhost:8082",
        "name": "On-chain Analytics Worker",
        "specialty": "链上交易分析、钱包行为、合约调用统计",
        "keywords": ["wallet", "transaction", "address", "合约", "钱包", "交易", "on-chain"],
        "price": "0.002 SETH",
        "latency": "medium",
        "description": "适合分析链上地址行为、追踪大额转账、统计合约调用频率",
    },
    "worker_smartmoney": {
        "url": "http://localhost:8083",
        "name": "Smart Money Tracker",
        "specialty": "机构资金动向、鲸鱼钱包追踪、聪明钱流入流出",
        "keywords": ["whale", "smart money", "机构", "鲸鱼", "大户", "聪明钱", "institutional"],
        "price": "0.003 SETH",
        "latency": "slow",
        "description": "追踪高胜率钱包和机构地址的持仓变动，适合判断资金进出趋势",
    },
}


def get_worker(worker_id: str) -> dict | None:
    """根据 ID 获取 Worker 信息"""
    return DATA_WORKERS.get(worker_id)


def list_workers_summary() -> list[dict]:
    """返回所有 Worker 的摘要，供 LLM 做选择"""
    return [
        {
            "worker_id": wid,
            "name": w["name"],
            "specialty": w["specialty"],
            "price": w["price"],
            "latency": w["latency"],
            "description": w["description"],
        }
        for wid, w in DATA_WORKERS.items()
    ]

"""
Data Worker Registry — 动态服务发现

不维护静态字典。Agent 启动时主动探测已知地址，
每个 Worker 通过 GET /catalog 返回自己的能力描述。

只有能连上、且 /catalog 正常响应的 Worker 才会出现在列表里。
Worker 下线 → 自动从列表消失，不需要手动维护。
"""
import requests

# 已知的 Worker 地址列表（相当于"我知道这些地址存在"）
# 生产环境里这个列表可以从链上注册表或 DNS 读取
KNOWN_WORKER_URLS = [
    "http://localhost:8081",  # DefiLlama Protocols Worker
    "http://localhost:8082",  # DefiLlama Yields Worker
    "http://localhost:8083",  # CoinGecko Market Worker
]

DISCOVERY_TIMEOUT = 2  # 秒，连不上就跳过


def discover_workers() -> list[dict]:
    """
    动态发现在线 Worker。

    向每个已知地址发 GET /catalog，收集能力描述。
    连接失败或超时的地址自动跳过（Worker 下线）。

    Returns:
        list: 在线 Worker 的能力信息，每项包含：
              worker_id, name, specialty, price, keywords, description, url
    """
    online = []
    for base_url in KNOWN_WORKER_URLS:
        try:
            resp = requests.get(f"{base_url}/catalog", timeout=DISCOVERY_TIMEOUT)
            if resp.status_code == 200:
                info = resp.json()
                info["url"] = base_url
                # 用 URL 的端口作为 worker_id（稳定、唯一）
                port = base_url.split(":")[-1]
                info["worker_id"] = f"worker_{port}"
                online.append(info)
        except requests.exceptions.ConnectionError:
            pass  # Worker 不在线，跳过
        except requests.exceptions.Timeout:
            pass  # 连接超时，跳过
        except Exception as e:
            print(f"  ⚠️  探测 {base_url} 失败: {e}")

    return online

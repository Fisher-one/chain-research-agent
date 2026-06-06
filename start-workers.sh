#!/bin/bash
# 启动三个真实 Data Worker（不同类型，调用不同 API）

echo "🚀 启动 Data Worker 集群..."
cd "$(dirname "$0")/server"

# Worker 1: DefiLlama Protocols — TVL & DEX Volume (最便宜)
WORKER_TYPE="defillama-protocols" PORT=":8081" go run main.go &
PID1=$!
echo "  ✅ Worker 8081 | DefiLlama Protocols  | 0.001 SETH | TVL & DEX 交易量"

sleep 0.5

# Worker 2: DefiLlama Yields — APY/收益率
WORKER_TYPE="defillama-yields" PORT=":8082" go run main.go &
PID2=$!
echo "  ✅ Worker 8082 | DefiLlama Yields     | 0.0015 SETH | 收益率 & APY"

sleep 0.5

# Worker 3: CoinGecko — 代币价格 & 市值 (最贵，覆盖最广)
WORKER_TYPE="coingecko" PORT=":8083" go run main.go &
PID3=$!
echo "  ✅ Worker 8083 | CoinGecko Market     | 0.002 SETH  | 代币价格 & 市值"

echo ""
echo "⏳ 等待 Worker 就绪..."
sleep 3

echo ""
echo "📡 验证 Worker 在线状态:"
for port in 8081 8082 8083; do
    name=$(curl -s "http://localhost:$port/catalog" 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['name'])" 2>/dev/null)
    if [ -n "$name" ]; then
        echo "  ✅ :$port — $name"
    else
        echo "  ❌ :$port — 未响应"
    fi
done

echo ""
echo "在新终端里运行 Agent："
echo "  cd agent && source .venv/bin/activate"
echo "  python3.11 main.py \"查一下 Uniswap 和 Curve 最近 7 天的交易量\""
echo ""
echo "按 Ctrl+C 停止所有 Worker"

trap "kill $PID1 $PID2 $PID3 2>/dev/null; echo '已停止所有 Worker'" INT
wait

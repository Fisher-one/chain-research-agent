#!/bin/bash
# 启动三个 Data Worker（同一个 Go server，不同端口和专长）

echo "🚀 启动 Data Worker 集群..."

cd "$(dirname "$0")/server"

# Worker 1: DefiLlama (端口 8081)
WORKER_NAME="DefiLlama Worker" PORT=":8081" go run main.go &
PID1=$!
echo "  ✅ Worker 1 (DefiLlama)     → http://localhost:8081  [0.001 SETH]"

# Worker 2: On-chain Analytics (端口 8082)
WORKER_NAME="On-chain Analytics" PORT=":8082" go run main.go &
PID2=$!
echo "  ✅ Worker 2 (On-chain)      → http://localhost:8082  [0.002 SETH]"

# Worker 3: Smart Money Tracker (端口 8083)
WORKER_NAME="Smart Money Tracker" PORT=":8083" go run main.go &
PID3=$!
echo "  ✅ Worker 3 (Smart Money)   → http://localhost:8083  [0.003 SETH]"

echo ""
echo "📡 三个 Worker 已就绪。在新终端里运行 Agent："
echo "   cd agent && source .venv/bin/activate"
echo "   python3.11 main.py \"查一下过去 7 天 Uniswap vs Curve 交易量\""
echo ""
echo "按 Ctrl+C 停止所有 Worker"

# 等待，Ctrl+C 时清理
trap "kill $PID1 $PID2 $PID3 2>/dev/null; echo '已停止所有 Worker'" INT
wait

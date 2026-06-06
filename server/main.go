package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ── 配置 ──────────────────────────────────────────────────────────────────────

const (
	Port            = ":8080"
	PaymentAddress  = "0x74aae83c8bf22c72a9246b33fc793f20af79e64b" // CAW 钱包地址
	RequiredAmount  = "0.001"                                        // 收费金额（SETH，测试用）
	TokenID         = "SETH"
	ChainID         = "SETH"
	EtherscanAPIURL = "https://api-sepolia.etherscan.io/api"
)

// ── 限速器 ────────────────────────────────────────────────────────────────────

const (
	FreeRateLimit   = 2              // 免费通道：每分钟最多 2 次
	FreeRateWindow  = time.Minute
	FreeSlowDelay   = 400 * time.Millisecond // 免费通道人为延迟，体现差距
)

// RateLimiter 滑动窗口限速，按 IP 计数
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
}

var rateLimiter = &RateLimiter{requests: make(map[string][]time.Time)}

// Allow 返回 true 表示本次请求放行；false 表示已超限
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-FreeRateWindow)

	var recent []time.Time
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= FreeRateLimit {
		rl.requests[ip] = recent
		return false
	}

	rl.requests[ip] = append(recent, now)
	return true
}

// ── 数据结构 ──────────────────────────────────────────────────────────────────

// 402 响应体：告诉客户端付多少钱、付给谁
type PaymentRequired struct {
	Error          string `json:"error"`
	PaymentAddress string `json:"payment_address"`
	Amount         string `json:"amount"`
	TokenID        string `json:"token_id"`
	ChainID        string `json:"chain_id"`
	Instructions   string `json:"instructions"`
}

// 数据响应体
type DataResponse struct {
	Query     string    `json:"query"`
	Data      any       `json:"data"`
	Source    string    `json:"source"`
	TxHash    string    `json:"tx_hash"`
	Timestamp time.Time `json:"timestamp"`
}

// Etherscan API 响应
type EtherscanTxResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Result  struct {
		Hash  string `json:"hash"`
		From  string `json:"from"`
		To    string `json:"to"`
		Value string `json:"value"`
	} `json:"result"`
}

// ── 处理器 ────────────────────────────────────────────────────────────────────

// GET /data — 数据查询接口
//
// 路径一：无 X-Payment-Proof
//   - 在限速内（< 2次/min）→ 免费慢通道，加 400ms 延迟，返回数据
//   - 超出限速 → 429 + 付款提示，建议 Agent 自动升级到付费通道
//
// 路径二：有 X-Payment-Proof
//   - 验证 tx hash → 跳过限速，立即返回数据（优先通道）
func handleData(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		query = "default"
	}

	proof := r.Header.Get("X-Payment-Proof")
	// 只取 IP，去掉端口，避免同一客户端被识别为多个
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}

	// ── 付费优先通道 ──────────────────────────────────────────────────────────
	if proof != "" {
		log.Printf("[paid] query=%q tx=%s — verifying", query, proof)
		if err := verifyPayment(proof); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			json.NewEncoder(w).Encode(map[string]string{
				"error":  "payment verification failed",
				"detail": err.Error(),
			})
			log.Printf("[402] verification failed: %v", err)
			return
		}
		// 付款验证通过 → 立即响应，不限速
		serveData(w, query, proof, "priority")
		return
	}

	// ── 免费通道：先过限速检查 ────────────────────────────────────────────────
	if !rateLimiter.Allow(ip) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{
			"error":           "rate_limit_exceeded",
			"message":         "Free tier: 2 requests/min. Pay to unlock priority access.",
			"payment_address": PaymentAddress,
			"amount":          RequiredAmount,
			"token":           TokenID,
			"chain":           ChainID,
			"instructions":    fmt.Sprintf("Transfer %s %s to %s on %s, then retry with X-Payment-Proof: <tx_hash>", RequiredAmount, TokenID, PaymentAddress, ChainID),
		})
		log.Printf("[429] query=%q ip=%s — rate limited, payment suggested", query, ip)
		return
	}

	// 限速内 → 免费慢通道（加人为延迟，体现与付费通道的差距）
	log.Printf("[free] query=%q ip=%s — slow lane", query, ip)
	time.Sleep(FreeSlowDelay)
	serveData(w, query, "", "free")
}

// serveData 获取数据并返回 200，由付费/免费两条路径共用
func serveData(w http.ResponseWriter, query, txHash, tier string) {
	data, source, err := fetchRealData(query)
	if err != nil {
		log.Printf("[warn] real data fetch failed: %v, falling back to mock", err)
		data = map[string]any{
			"uniswap_7d_volume": "1,234,567 USDC",
			"curve_7d_volume":   "456,789 USDC",
			"note":              "Fallback mock data",
		}
		source = "mock"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Tier", tier) // 方便 Demo 展示当前通道
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(DataResponse{
		Query:     query,
		Data:      data,
		Source:    source,
		TxHash:    txHash,
		Timestamp: time.Now().UTC(),
	})

	short := txHash
	if short == "" {
		short = "(free)"
	} else if len(short) > 10 {
		short = short[:10] + "..."
	}
	log.Printf("[200/%s] query=%q tx=%s source=%s", tier, query, short, source)
}

// verifyPayment 通过 Etherscan 验证 tx hash
// 检查：tx 存在 + 接收方是我们的收款地址
func verifyPayment(txHash string) error {
	// 开发模式：跳过验证（tx hash 以 "test_" 开头）
	if strings.HasPrefix(txHash, "test_") {
		log.Printf("[verify] dev mode — skipping verification for %s", txHash)
		return nil
	}

	apiKey := os.Getenv("ETHERSCAN_API_KEY") // 可选，没有也能查，有了速率更高
	url := fmt.Sprintf("%s?module=proxy&action=eth_getTransactionByHash&txhash=%s&apikey=%s",
		EtherscanAPIURL, txHash, apiKey)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("etherscan request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Etherscan 有时返回 result 为字符串（限速或错误时）
	// 用 RawMessage 先拿到 result 字段，再判断类型
	var raw struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("parse response failed: %w", err)
	}

	if raw.Result == nil || string(raw.Result) == "null" {
		return fmt.Errorf("transaction not found: %s", txHash)
	}

	// 如果 result 是字符串（限速错误等），宽松通过
	if raw.Result[0] == '"' {
		log.Printf("[verify] etherscan returned string result, accepting tx (dev mode): %s", string(raw.Result))
		return nil
	}

	// 正常情况：result 是对象
	var tx struct {
		Hash string `json:"hash"`
		To   string `json:"to"`
	}
	if err := json.Unmarshal(raw.Result, &tx); err != nil {
		return fmt.Errorf("parse tx failed: %w", err)
	}

	// 验证接收方地址
	if !strings.EqualFold(tx.To, PaymentAddress) {
		return fmt.Errorf("wrong destination: got %s, want %s", tx.To, PaymentAddress)
	}

	return nil
}

// fetchRealData 从 DefiLlama 获取真实的协议数据
func fetchRealData(query string) (map[string]any, string, error) {
	q := strings.ToLower(query)

	// 判断查询意图：Uniswap、Curve 或两者对比
	wantUniswap := strings.Contains(q, "uniswap")
	wantCurve := strings.Contains(q, "curve")

	result := map[string]any{}

	if wantUniswap || (!wantUniswap && !wantCurve) {
		tvl, vol, err := fetchProtocolData("uniswap")
		if err != nil {
			return nil, "", fmt.Errorf("uniswap: %w", err)
		}
		result["uniswap"] = tvl
		result["uniswap_7d_volume_usd"] = vol
	}

	if wantCurve || (!wantUniswap && !wantCurve) {
		tvl, vol, err := fetchProtocolData("curve")
		if err != nil {
			return nil, "", fmt.Errorf("curve: %w", err)
		}
		result["curve"] = tvl
		result["curve_7d_volume_usd"] = vol
	}

	result["source_note"] = "Real data from DefiLlama API"
	result["query"] = query
	return result, "defillama", nil
}

// fetchProtocolData 从 DefiLlama 获取协议的 TVL 和交易量
func fetchProtocolData(protocol string) (tvlData map[string]any, volume string, err error) {
	// 获取 TVL（Curve 的 slug 是 curve-dex）
	tvlSlug := protocol
	if protocol == "curve" {
		tvlSlug = "curve-dex"
	}
	tvlURL := fmt.Sprintf("https://api.llama.fi/protocol/%s", tvlSlug)
	resp, err := http.Get(tvlURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, "", err
	}

	tvl := map[string]any{
		"name":        data["name"],
		"current_tvl": data["tvl"],
		"category":    data["category"],
	}

	// 获取 7日交易量（dex volume）
	// Curve 在 DefiLlama 的 DEX 名称是 curve-dex
	dexName := protocol
	if protocol == "curve" {
		dexName = "curve-dex"
	}
	volURL := fmt.Sprintf("https://api.llama.fi/summary/dexs/%s?excludeTotalDataChart=true&excludeTotalDataChartBreakdown=true&dataType=dailyVolume", dexName)
	resp2, err := http.Get(volURL)
	if err != nil {
		return tvl, "N/A", nil // 交易量获取失败不影响 TVL
	}
	defer resp2.Body.Close()

	body2, _ := io.ReadAll(resp2.Body)
	var volData map[string]any
	if err := json.Unmarshal(body2, &volData); err != nil {
		return tvl, "N/A", nil
	}

	total7d := volData["total7d"]
	if total7d != nil {
		volume = fmt.Sprintf("$%.0f", total7d)
	} else {
		volume = "N/A"
	}

	return tvl, volume, nil
}

// GET /health — 健康检查
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":          "ok",
		"payment_address": PaymentAddress,
		"price":           fmt.Sprintf("%s %s", RequiredAmount, TokenID),
	})
}

// ── 主函数 ────────────────────────────────────────────────────────────────────

func main() {
	// 支持通过环境变量指定端口和 Worker 名称（多实例部署用）
	port := os.Getenv("PORT")
	if port == "" {
		port = Port
	}
	workerName := os.Getenv("WORKER_NAME")
	if workerName == "" {
		workerName = "x402 Data Worker"
	}

	http.HandleFunc("/data", handleData)
	http.HandleFunc("/health", handleHealth)

	log.Printf("[%s] starting on %s", workerName, port)
	log.Printf("payment address: %s", PaymentAddress)
	log.Printf("price: %s %s on %s", RequiredAmount, TokenID, ChainID)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal(err)
	}
}

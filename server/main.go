package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ── 配置（从环境变量读取，支持多实例） ──────────────────────────────────────────

var (
	PaymentAddress  = "0x74aae83c8bf22c72a9246b33fc793f20af79e64b"
	EtherscanAPIURL = "https://api-sepolia.etherscan.io/api"

	// httpClient 带超时，防止外部 API 慢响应挂死 Worker
	httpClient = &http.Client{Timeout: 10 * time.Second}
)

// workerConfig 根据 WORKER_TYPE 决定当前实例的身份和价格
type workerConfig struct {
	Name        string
	Type        string
	Price       string // SETH
	TokenID     string
	ChainID     string
	Specialty   string
	Keywords    []string
	Description string
}

func loadWorkerConfig() workerConfig {
	t := os.Getenv("WORKER_TYPE")
	switch t {
	case "defillama-yields":
		return workerConfig{
			Name:        "DefiLlama Yields Worker",
			Type:        "defillama-yields",
			Price:       "0.0015",
			TokenID:     "SETH",
			ChainID:     "SETH",
			Specialty:   "DeFi 收益率与 APY 数据",
			Keywords:    []string{"yield", "apy", "apr", "收益", "利率", "借贷", "lending", "staking"},
			Description: "查询各 DeFi 协议的实时收益率（APY/APR），覆盖借贷、质押、流动性挖矿，数据来自 DefiLlama Yields API",
		}
	case "coingecko":
		return workerConfig{
			Name:        "CoinGecko Market Worker",
			Type:        "coingecko",
			Price:       "0.002",
			TokenID:     "SETH",
			ChainID:     "SETH",
			Specialty:   "代币价格、市值与 24h 涨跌幅",
			Keywords:    []string{"price", "价格", "market cap", "市值", "涨跌", "24h", "token", "币价"},
			Description: "查询代币实时价格、市值排名、24h 交易量和涨跌幅，数据来自 CoinGecko 免费 API",
		}
	default: // defillama-protocols（默认）
		return workerConfig{
			Name:        "DefiLlama Protocols Worker",
			Type:        "defillama-protocols",
			Price:       "0.001",
			TokenID:     "SETH",
			ChainID:     "SETH",
			Specialty:   "DeFi 协议 TVL 与 DEX 交易量",
			Keywords:    []string{"tvl", "volume", "交易量", "流动性", "dex", "uniswap", "curve", "defi", "protocol"},
			Description: "查询主流 DeFi 协议的 TVL（总锁仓量）和 DEX 7日交易量，数据来自 DefiLlama API，更新及时",
		}
	}
}

// ── 限速器 ────────────────────────────────────────────────────────────────────

const (
	FreeRateLimit = 2
	FreeRateWindow = time.Minute
	FreeSlowDelay  = 400 * time.Millisecond
)

type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
}

var rateLimiter = &RateLimiter{requests: make(map[string][]time.Time)}

// ── 数据缓存（避免反复调外部 API，Demo 时关键）──────────────────────────────────

type cacheEntry struct {
	data      map[string]any
	source    string
	fetchedAt time.Time
}

var (
	dataCache   = map[string]cacheEntry{}
	dataCacheMu sync.RWMutex
	cacheTTL    = 5 * time.Minute
)

func cachedFetch(key string, fetch func() (map[string]any, string, error)) (map[string]any, string, error) {
	dataCacheMu.RLock()
	if entry, ok := dataCache[key]; ok && time.Since(entry.fetchedAt) < cacheTTL {
		dataCacheMu.RUnlock()
		return entry.data, entry.source, nil
	}
	dataCacheMu.RUnlock()

	data, source, err := fetch()
	if err != nil {
		return nil, "", err
	}
	dataCacheMu.Lock()
	dataCache[key] = cacheEntry{data: data, source: source, fetchedAt: time.Now()}
	dataCacheMu.Unlock()
	return data, source, nil
}

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

type DataResponse struct {
	Query     string    `json:"query"`
	Data      any       `json:"data"`
	Source    string    `json:"source"`
	TxHash    string    `json:"tx_hash"`
	Timestamp time.Time `json:"timestamp"`
}

// ── 处理器 ────────────────────────────────────────────────────────────────────

var cfg workerConfig

// GET /catalog — Worker 自我描述，供 Agent 动态发现
func handleCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"name":            cfg.Name,
		"worker_type":     cfg.Type,
		"specialty":       cfg.Specialty,
		"keywords":        cfg.Keywords,
		"price":           fmt.Sprintf("%s %s", cfg.Price, cfg.TokenID),
		"price_amount":    cfg.Price,
		"token_id":        cfg.TokenID,
		"chain_id":        cfg.ChainID,
		"payment_address": PaymentAddress,
		"description":     cfg.Description,
		"status":          "online",
	})
}

// GET /health — 健康检查
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":          "ok",
		"worker":          cfg.Name,
		"payment_address": PaymentAddress,
		"price":           fmt.Sprintf("%s %s", cfg.Price, cfg.TokenID),
	})
}

// GET /data — 数据查询接口（x402 保护）
func handleData(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		query = "default"
	}

	proof := r.Header.Get("X-Payment-Proof")
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}

	// 付费优先通道
	if proof != "" {
		log.Printf("[paid] query=%q tx=%s", query, proof)
		if err := verifyPayment(proof); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "payment verification failed", "detail": err.Error(),
			})
			return
		}
		serveData(w, query, proof, "priority")
		return
	}

	// 免费通道：限速检查
	if !rateLimiter.Allow(ip) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{
			"error":           "rate_limit_exceeded",
			"message":         "Free tier: 2 requests/min. Pay to unlock priority access.",
			"payment_address": PaymentAddress,
			"amount":          cfg.Price,
			"token_id":        cfg.TokenID,
			"chain":           cfg.ChainID,
			"instructions":    fmt.Sprintf("Transfer %s %s to %s on %s, retry with X-Payment-Proof: <tx_hash>", cfg.Price, cfg.TokenID, PaymentAddress, cfg.ChainID),
		})
		log.Printf("[429] query=%q ip=%s", query, ip)
		return
	}

	log.Printf("[free] query=%q ip=%s", query, ip)
	time.Sleep(FreeSlowDelay)
	serveData(w, query, "", "free")
}

func serveData(w http.ResponseWriter, query, txHash, tier string) {
	data, source, err := fetchData(query)
	if err != nil {
		log.Printf("[warn] fetch failed: %v", err)
		http.Error(w, `{"error":"data fetch failed","detail":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Tier", tier)
	w.Header().Set("X-Worker", cfg.Name)
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

// ── 数据获取：根据 WORKER_TYPE 调用不同 API ────────────────────────────────────

func fetchData(query string) (map[string]any, string, error) {
	// 缓存 key：Worker 类型（同一 Worker 的数据按类型缓存，不按 query，因为数据是全量的）
	cacheKey := cfg.Type
	switch cfg.Type {
	case "defillama-yields":
		return cachedFetch(cacheKey, func() (map[string]any, string, error) {
			return fetchYieldsData(query)
		})
	case "coingecko":
		return cachedFetch(cacheKey, func() (map[string]any, string, error) {
			return fetchCoinGeckoData(query)
		})
	default:
		return cachedFetch(cacheKey, func() (map[string]any, string, error) {
			return fetchProtocolsData(query)
		})
	}
}

// Worker 1: DefiLlama 协议 TVL + DEX 交易量
func fetchProtocolsData(query string) (map[string]any, string, error) {
	q := strings.ToLower(query)
	wantUniswap := strings.Contains(q, "uniswap")
	wantCurve := strings.Contains(q, "curve")
	result := map[string]any{}

	if wantUniswap || (!wantUniswap && !wantCurve) {
		tvl, vol, err := fetchProtocolTVL("uniswap")
		if err != nil {
			return nil, "", fmt.Errorf("uniswap: %w", err)
		}
		result["uniswap"] = tvl
		result["uniswap_7d_volume_usd"] = vol
	}
	if wantCurve || (!wantUniswap && !wantCurve) {
		tvl, vol, err := fetchProtocolTVL("curve")
		if err != nil {
			return nil, "", fmt.Errorf("curve: %w", err)
		}
		result["curve"] = tvl
		result["curve_7d_volume_usd"] = vol
	}
	result["data_type"] = "TVL & DEX Volume"
	return result, "defillama-protocols", nil
}

func fetchProtocolTVL(protocol string) (map[string]any, string, error) {
	slug := protocol
	if protocol == "curve" {
		slug = "curve-dex"
	}
	resp, err := httpClient.Get("https://api.llama.fi/protocol/" + slug)
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
		"name": data["name"], "current_tvl": data["tvl"], "category": data["category"],
	}

	dexName := slug
	volURL := fmt.Sprintf("https://api.llama.fi/summary/dexs/%s?excludeTotalDataChart=true&excludeTotalDataChartBreakdown=true&dataType=dailyVolume", dexName)
	resp2, err := httpClient.Get(volURL)
	if err != nil {
		return tvl, "N/A", nil
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	var volData map[string]any
	json.Unmarshal(body2, &volData)
	vol := "N/A"
	if v := volData["total7d"]; v != nil {
		vol = fmt.Sprintf("$%.0f", v)
	}
	return tvl, vol, nil
}

// Worker 2: DefiLlama Yields — APY/收益率数据
func fetchYieldsData(query string) (map[string]any, string, error) {
	resp, err := httpClient.Get("https://yields.llama.fi/pools")
	if err != nil {
		return nil, "", fmt.Errorf("yields API failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var raw struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, "", fmt.Errorf("parse yields failed: %w", err)
	}

	q := strings.ToLower(query)

	// 按 query 关键词过滤相关协议，取 APY 前10
	var matched []map[string]any
	for _, pool := range raw.Data {
		project := strings.ToLower(fmt.Sprintf("%v", pool["project"]))
		symbol := strings.ToLower(fmt.Sprintf("%v", pool["symbol"]))
		chain := strings.ToLower(fmt.Sprintf("%v", pool["chain"]))

		if strings.Contains(project, "uniswap") && strings.Contains(q, "uniswap") ||
			strings.Contains(project, "curve") && strings.Contains(q, "curve") ||
			strings.Contains(project, "aave") && strings.Contains(q, "aave") ||
			strings.Contains(project, "compound") && strings.Contains(q, "compound") ||
			strings.Contains(symbol, "eth") && strings.Contains(q, "eth") ||
			strings.Contains(chain, "ethereum") && (strings.Contains(q, "ethereum") || strings.Contains(q, "eth")) {
			matched = append(matched, map[string]any{
				"project": pool["project"],
				"symbol":  pool["symbol"],
				"chain":   pool["chain"],
				"apy":     pool["apy"],
				"tvlUsd":  pool["tvlUsd"],
			})
		}
		if len(matched) >= 10 {
			break
		}
	}

	// 如果没有关键词匹配，返回全网 APY 最高的10个
	if len(matched) == 0 {
		type poolAPY struct {
			data map[string]any
			apy  float64
		}
		var pools []poolAPY
		for _, pool := range raw.Data {
			apy, _ := pool["apy"].(float64)
			if apy > 0 && apy < 1000 { // 过滤异常值
				pools = append(pools, poolAPY{pool, apy})
			}
		}
		// 简单取前10高APY
		count := 0
		for i := len(pools) - 1; i >= 0 && count < 10; i-- {
			p := pools[i]
			matched = append(matched, map[string]any{
				"project": p.data["project"],
				"symbol":  p.data["symbol"],
				"chain":   p.data["chain"],
				"apy":     p.data["apy"],
				"tvlUsd":  p.data["tvlUsd"],
			})
			count++
		}
	}

	return map[string]any{
		"data_type":   "DeFi Yields & APY",
		"query":       query,
		"pools":       matched,
		"total_pools": len(raw.Data),
	}, "defillama-yields", nil
}

// Worker 3: CoinGecko — 代币价格、市值
func fetchCoinGeckoData(query string) (map[string]any, string, error) {
	// 从 query 里提取代币名称，映射到 CoinGecko ID
	coinMap := map[string]string{
		"uniswap": "uniswap",
		"uni":     "uniswap",
		"curve":   "curve-dao-token",
		"crv":     "curve-dao-token",
		"aave":    "aave",
		"eth":     "ethereum",
		"ethereum": "ethereum",
		"btc":     "bitcoin",
		"bitcoin": "bitcoin",
		"matic":   "matic-network",
		"polygon": "matic-network",
		"arb":     "arbitrum",
		"arbitrum": "arbitrum",
		"op":      "optimism",
		"optimism": "optimism",
	}

	q := strings.ToLower(query)
	var ids []string
	seen := map[string]bool{}
	for keyword, cgID := range coinMap {
		if strings.Contains(q, keyword) && !seen[cgID] {
			ids = append(ids, cgID)
			seen[cgID] = true
		}
	}
	// 默认查几个主流代币
	if len(ids) == 0 {
		ids = []string{"uniswap", "curve-dao-token", "aave", "ethereum"}
	}

	apiURL := fmt.Sprintf(
		"https://api.coingecko.com/api/v3/simple/price?ids=%s&vs_currencies=usd&include_24hr_vol=true&include_24hr_change=true&include_market_cap=true",
		url.QueryEscape(strings.Join(ids, ",")),
	)

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return nil, "", fmt.Errorf("CoinGecko API failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return nil, "", fmt.Errorf("CoinGecko rate limited, try again in 60s")
	}

	body, _ := io.ReadAll(resp.Body)
	var prices map[string]map[string]any
	if err := json.Unmarshal(body, &prices); err != nil {
		return nil, "", fmt.Errorf("parse CoinGecko response failed: %w", err)
	}

	return map[string]any{
		"data_type": "Token Prices & Market Data",
		"query":     query,
		"prices":    prices,
		"source_note": "Real-time data from CoinGecko free API",
	}, "coingecko", nil
}

// ── 支付验证 ──────────────────────────────────────────────────────────────────

func verifyPayment(txHash string) error {
	if strings.HasPrefix(txHash, "test_") {
		log.Printf("[verify] dev mode — skip for %s", txHash)
		return nil
	}
	apiKey := os.Getenv("ETHERSCAN_API_KEY")
	apiURL := fmt.Sprintf("%s?module=proxy&action=eth_getTransactionByHash&txhash=%s&apikey=%s",
		EtherscanAPIURL, txHash, apiKey)
	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return fmt.Errorf("etherscan request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var raw struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("parse response failed: %w", err)
	}
	if raw.Result == nil || string(raw.Result) == "null" {
		return fmt.Errorf("transaction not found: %s", txHash)
	}
	if raw.Result[0] == '"' {
		log.Printf("[verify] etherscan rate limited, accepting tx: %s", txHash)
		return nil
	}
	var tx struct {
		To string `json:"to"`
	}
	if err := json.Unmarshal(raw.Result, &tx); err != nil {
		return fmt.Errorf("parse tx failed: %w", err)
	}
	if !strings.EqualFold(tx.To, PaymentAddress) {
		return fmt.Errorf("wrong destination: got %s, want %s", tx.To, PaymentAddress)
	}
	return nil
}

// ── 主函数 ────────────────────────────────────────────────────────────────────

func main() {
	cfg = loadWorkerConfig()

	port := os.Getenv("PORT")
	if port == "" {
		port = ":8080"
	}

	http.HandleFunc("/catalog", handleCatalog)
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/data", handleData)

	log.Printf("[%s] starting on %s | specialty: %s | price: %s %s",
		cfg.Name, port, cfg.Specialty, cfg.Price, cfg.TokenID)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal(err)
	}
}

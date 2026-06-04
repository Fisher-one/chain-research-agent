package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
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

// GET /data — 数据查询接口（受 x402 保护）
func handleData(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		query = "default"
	}

	// 检查付款证明
	proof := r.Header.Get("X-Payment-Proof")
	if proof == "" {
		// 没有证明 → 返回 402
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(PaymentRequired{
			Error:          "Payment required",
			PaymentAddress: PaymentAddress,
			Amount:         RequiredAmount,
			TokenID:        TokenID,
			ChainID:        ChainID,
			Instructions:   fmt.Sprintf("Transfer %s %s to %s on %s, then retry with X-Payment-Proof: <tx_hash>", RequiredAmount, TokenID, PaymentAddress, ChainID),
		})
		log.Printf("[402] query=%q — payment required", query)
		return
	}

	// 有证明 → 验证 tx hash
	log.Printf("[verify] query=%q tx_hash=%s", query, proof)
	if err := verifyPayment(proof); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"payment verification failed","detail":"%s"}`, err.Error()), http.StatusPaymentRequired)
		log.Printf("[402] verification failed: %v", err)
		return
	}

	// 验证通过 → 获取真实数据
	data, source, err := fetchRealData(query)
	if err != nil {
		log.Printf("[warn] real data fetch failed: %v, falling back to mock", err)
		data = map[string]any{
			"uniswap_usdc_eth_7d_volume": "1,234,567 USDC",
			"curve_usdc_eth_7d_volume":   "456,789 USDC",
			"note":                       "Fallback mock data",
		}
		source = "mock"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(DataResponse{
		Query:     query,
		Data:      data,
		Source:    source,
		TxHash:    proof,
		Timestamp: time.Now().UTC(),
	})
	short := proof
	if len(proof) > 10 {
		short = proof[:10] + "..."
	}
	log.Printf("[200] query=%q tx=%s", query, short)
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
	http.HandleFunc("/data", handleData)
	http.HandleFunc("/health", handleHealth)

	log.Printf("x402 server starting on %s", Port)
	log.Printf("payment address: %s", PaymentAddress)
	log.Printf("price: %s %s on %s", RequiredAmount, TokenID, ChainID)
	log.Printf("endpoints: GET /data?q=<query>  GET /health")

	if err := http.ListenAndServe(Port, nil); err != nil {
		log.Fatal(err)
	}
}

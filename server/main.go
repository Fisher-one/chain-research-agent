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

	// 验证通过 → 返回 mock 数据
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(DataResponse{
		Query: query,
		Data: map[string]any{
			"uniswap_usdc_eth_7d_volume": "1,234,567 USDC",
			"curve_usdc_eth_7d_volume":   "456,789 USDC",
			"note":                       "Mock data — replace with real Dune API call",
		},
		Source:    "mock",
		TxHash:    proof,
		Timestamp: time.Now().UTC(),
	})
	log.Printf("[200] query=%q tx=%s", query, proof[:10]+"...")
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
	var result struct {
		Result *struct {
			Hash string `json:"hash"`
			To   string `json:"to"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse response failed: %w", err)
	}

	if result.Result == nil {
		return fmt.Errorf("transaction not found: %s", txHash)
	}

	// 验证接收方地址
	if !strings.EqualFold(result.Result.To, PaymentAddress) {
		return fmt.Errorf("wrong destination: got %s, want %s", result.Result.To, PaymentAddress)
	}

	return nil
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

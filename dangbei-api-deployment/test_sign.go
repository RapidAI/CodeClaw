package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

func generateSign(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		if v := params[k]; v != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
	}
	signStr := strings.Join(parts, "&")
	
	fmt.Println("Sign string:", signStr)
	
	hash := md5.Sum([]byte(signStr))
	return strings.ToUpper(hex.EncodeToString(hash[:]))
}

func main() {
	// 从实际请求中看到的参数
	params := map[string]string{
		"timestamp": "1771480812",
		"nonce":     "QKprvU3gsvbXc19ylUA9R",
		"token":     "5479caa4e0b548d998f960ec0200e132",
	}
	
	sign := generateSign(params)
	fmt.Println("Generated sign:", sign)
	fmt.Println("Expected sign:  8EFEEBCF0549716D478F5A02F589931D")
	fmt.Println("Match:", sign == "8EFEEBCF0549716D478F5A02F589931D")
}

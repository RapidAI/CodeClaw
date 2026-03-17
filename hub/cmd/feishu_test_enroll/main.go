package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	appID     = "cli_a93c1bd53cb8dbcd"
	appSecret = "mPzzw0JijylOwyFUBjDywcJzVkS133fa"
	apiBase   = "https://open.feishu.cn"

	testEmail   = "znsoft" + "@" + "gmail.com"
	testMobile  = "+8615646550398"
	testName    = "Daniel"
	deptName    = "MaClaw"
	deptID_     = "9d74g56d86ge523f" // MaClaw department ID (known)
)

var client = &http.Client{Timeout: 15 * time.Second}

func main() {
	// Step 1: Get tenant_access_token
	fmt.Println("=== Step 1: 获取 tenant_access_token ===")
	token, err := getTenantToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取 token 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ token: %s...%s\n\n", token[:8], token[len(token)-4:])

	// Step 2: Use known MaClaw department ID
	fmt.Println("=== Step 2: 使用 MaClaw 部门 ===")
	deptID := deptID_
	fmt.Printf("✅ 部门 %s ID: %s\n\n", deptName, deptID)

	// Step 3: Check if user already exists
	fmt.Println("=== Step 3: 检查用户是否已存在 ===")
	openID, err := lookupByEmail(token, testEmail)
	if err != nil {
		fmt.Printf("⚠️  邮箱查找失败: %v\n", err)
	}
	if openID != "" {
		fmt.Printf("✅ 用户已存在, open_id=%s\n", openID)
		fmt.Println("无需创建，测试完成。")
		return
	}
	fmt.Println("用户不存在，继续创建...")

	// Step 4: Create user
	fmt.Println("=== Step 4: 创建用户 ===")
	newOpenID, err := createUser(token, testEmail, testMobile, testName, deptID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 创建用户失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 用户创建成功! open_id=%s\n", newOpenID)
}

func getTenantToken() (string, error) {
	body, _ := json.Marshal(map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	})
	resp, err := client.Post(apiBase+"/open-apis/auth/v3/tenant_access_token/internal", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("code=%d msg=%s", result.Code, result.Msg)
	}
	return result.TenantAccessToken, nil
}

func findDepartment(token, name string) (string, error) {
	// Search departments under root (fetch_child=true to get names)
	url := apiBase + "/open-apis/contact/v3/departments?parent_department_id=0&fetch_child=true&page_size=50"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	fmt.Printf("部门列表响应: %s\n\n", string(raw))

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []struct {
				Name         string `json:"name"`
				DepartmentID string `json:"department_id"`
				OpenDeptID   string `json:"open_department_id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("code=%d msg=%s", result.Code, result.Msg)
	}
	for _, dept := range result.Data.Items {
		if dept.Name == name {
			if dept.OpenDeptID != "" {
				return dept.OpenDeptID, nil
			}
			return dept.DepartmentID, nil
		}
	}
	return "", fmt.Errorf("部门 %q 未找到", name)
}

func lookupByEmail(token, email string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"emails": []string{email},
	})
	req, _ := http.NewRequest("POST", apiBase+"/open-apis/contact/v3/users/batch_get_id?user_id_type=open_id", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	fmt.Printf("邮箱查找响应: %s\n\n", string(raw))

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UserList []struct {
				UserID string `json:"user_id"`
			} `json:"user_list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("code=%d msg=%s", result.Code, result.Msg)
	}
	if len(result.Data.UserList) > 0 && result.Data.UserList[0].UserID != "" {
		return result.Data.UserList[0].UserID, nil
	}
	return "", nil
}

func createUser(token, email, mobile, name, deptID string) (string, error) {
	payload := map[string]any{
		"name":           name,
		"email":          email,
		"mobile":         mobile,
		"department_ids": []string{deptID},
		"employee_type":  1,
	}
	body, _ := json.Marshal(payload)
	fmt.Printf("创建用户请求: %s\n", string(body))

	req, _ := http.NewRequest("POST", apiBase+"/open-apis/contact/v3/users?user_id_type=open_id&department_id_type=department_id", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	fmt.Printf("创建用户响应: %s\n\n", string(raw))

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			User struct {
				OpenID string `json:"open_id"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("code=%d msg=%s", result.Code, result.Msg)
	}
	return result.Data.User.OpenID, nil
}

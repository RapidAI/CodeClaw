package tool

import (
	"fmt"
	"testing"
)

func makeToolDef(name, description string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": description,
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}
}

func TestRouter_BM25_ChineseQuery(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	// Build 35 tools (exceeds MaxToolBudget=28) to trigger routing.
	var tools []map[string]interface{}
	// Add all core tools first.
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core tool "+name))
	}
	// Add non-core candidates.
	tools = append(tools,
		makeToolDef("database_query", "执行数据库查询，支持 SQL 语句"),
		makeToolDef("git_commit", "提交代码到 Git 仓库"),
		makeToolDef("deploy_service", "部署服务到生产环境"),
		makeToolDef("network_scan", "扫描网络端口和服务"),
		makeToolDef("log_analyzer", "分析日志文件查找错误"),
		makeToolDef("image_resize", "调整图片大小和格式"),
		makeToolDef("email_send", "发送邮件通知"),
		makeToolDef("cache_clear", "清除缓存数据"),
		makeToolDef("backup_create", "创建数据备份"),
		makeToolDef("monitor_health", "监控服务健康状态"),
		makeToolDef("translate_text", "翻译文本内容"),
		makeToolDef("compress_file", "压缩文件和目录"),
		makeToolDef("schedule_task", "创建定时任务"),
		makeToolDef("search_code", "搜索代码库中的内容"),
		makeToolDef("format_code", "格式化代码文件"),
		makeToolDef("test_runner", "运行测试套件"),
		makeToolDef("doc_generator", "生成文档"),
		makeToolDef("api_tester", "测试 API 接口"),
		makeToolDef("perf_profiler", "性能分析工具"),
		makeToolDef("security_scan", "安全漏洞扫描"),
	)

	if len(tools) <= MaxToolBudget {
		t.Fatalf("need more than %d tools to test routing, got %d", MaxToolBudget, len(tools))
	}

	result := router.Route("我要查询数据库", tools)
	if len(result) > MaxToolBudget+2 { // +2 for possible recommendation hint
		t.Errorf("result should be within budget, got %d", len(result))
	}

	// database_query should be in the result since the query is about databases.
	found := false
	for _, r := range result {
		if ExtractToolName(r) == "database_query" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(result))
		for i, r := range result {
			names[i] = ExtractToolName(r)
		}
		t.Errorf("database_query should be selected for '我要查询数据库', got: %v", names)
	}
}

func TestRouter_BM25_EmptyMessage(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.Route("", tools)
	// Should still return results (BM25 returns nil scores, all get 0, still fills budget).
	if len(result) == 0 {
		t.Error("empty message should still return tools")
	}
}

func TestDynamicToolBuilder_BM25(t *testing.T) {
	reg := NewRegistry()
	reg.Register(RegisteredTool{Name: "bash", Description: "run shell", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "read_file", Description: "read a file", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "db_query", Description: "执行数据库 SQL 查询", Category: CategoryNonCode, Tags: []string{"database", "sql"}})
	reg.Register(RegisteredTool{Name: "git_push", Description: "推送代码到远程仓库", Category: CategoryNonCode, Tags: []string{"git", "vcs"}})
	reg.Register(RegisteredTool{Name: "deploy", Description: "部署服务", Category: CategoryNonCode, Tags: []string{"deploy"}})
	// Add enough tools to trigger filtering.
	for i := 0; i < 20; i++ {
		reg.Register(RegisteredTool{
			Name:        fmt.Sprintf("filler_%d", i),
			Description: fmt.Sprintf("filler tool number %d", i),
			Category:    CategoryNonCode,
		})
	}

	builder := NewDynamicToolBuilder(reg)
	result := builder.Build("数据库查询")

	// db_query should be in the result.
	found := false
	for _, def := range result {
		if ExtractToolName(def) == "db_query" {
			found = true
			break
		}
	}
	if !found {
		t.Error("db_query should be selected for '数据库查询'")
	}
}

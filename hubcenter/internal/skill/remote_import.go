package skill

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// RemoteImporter 从远程 URL 抓取 skill 并转换为 maclaw 格式。
type RemoteImporter struct {
	client *http.Client
}

func NewRemoteImporter() *RemoteImporter {
	return &RemoteImporter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// ImportResult 表示一次导入的结果。
type ImportResult struct {
	Skills []HubSkillFull `json:"skills"`
	Errors []string       `json:"errors,omitempty"`
}

// gitHubRepo 解析 GitHub 仓库 URL 为 owner/repo/branch/subpath。
type gitHubRepo struct {
	Owner   string
	Repo    string
	Branch  string // 为空时自动尝试 main/master
	SubPath string // 子路径，如 "skills/foo"
}

var ghRepoRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?(?:/tree/([^/]+)(/.*)?)?/?$`)

func parseGitHubURL(rawURL string) (*gitHubRepo, bool) {
	m := ghRepoRe.FindStringSubmatch(rawURL)
	if m == nil {
		return nil, false
	}
	subPath := strings.TrimPrefix(m[4], "/")
	return &gitHubRepo{Owner: m[1], Repo: m[2], Branch: m[3], SubPath: subPath}, true
}

// ImportFromURL 从 URL 导入 skill(s)。
// 支持：
//   - GitHub 仓库 URL → 递归扫描子目录找 skill.yaml
//   - 直接 raw URL 指向 skill.yaml
func (ri *RemoteImporter) ImportFromURL(rawURL string) (*ImportResult, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("URL is empty")
	}
	if gh, ok := parseGitHubURL(rawURL); ok {
		return ri.importFromGitHub(gh, rawURL)
	}
	return ri.importFromRawURL(rawURL)
}

// importFromGitHub 使用 GitHub API 递归扫描仓库目录树。
func (ri *RemoteImporter) importFromGitHub(gh *gitHubRepo, sourceURL string) (*ImportResult, error) {
	branches := []string{gh.Branch}
	if gh.Branch == "" {
		branches = []string{"main", "master"}
	}
	for _, branch := range branches {
		result, err := ri.scanGitHubTree(gh.Owner, gh.Repo, branch, gh.SubPath, sourceURL)
		if err != nil {
			log.Printf("[remote_import] branch %s failed: %v", branch, err)
			continue
		}
		return result, nil
	}
	return nil, fmt.Errorf("failed to access GitHub repo %s/%s (tried branches: %v); note: unauthenticated GitHub API is limited to 60 requests/hour", gh.Owner, gh.Repo, branches)
}

// ghTreeEntry 是 GitHub Trees API 返回的条目。
type ghTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
}

type ghTreeResponse struct {
	Tree      []ghTreeEntry `json:"tree"`
	Truncated bool          `json:"truncated"`
}

func (ri *RemoteImporter) scanGitHubTree(owner, repo, branch, subPath, sourceURL string) (*ImportResult, error) {
	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, branch)
	body, err := ri.httpGet(treeURL)
	if err != nil {
		return nil, fmt.Errorf("fetch tree: %w", err)
	}
	var tree ghTreeResponse
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, fmt.Errorf("parse tree: %w", err)
	}

	result := &ImportResult{}
	for _, entry := range tree.Tree {
		if entry.Type != "blob" {
			continue
		}
		base := path.Base(entry.Path)
		if base != "skill.yaml" && base != "skill.yml" {
			continue
		}
		// 如果指定了子路径，只匹配该子路径下的
		if subPath != "" && !strings.HasPrefix(entry.Path, subPath) {
			continue
		}

		rawContentURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, entry.Path)
		yamlData, err := ri.httpGet(rawContentURL)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("fetch %s: %v", entry.Path, err))
			continue
		}

		skillDir := path.Dir(entry.Path)

		sk, err := ri.parseSkillYAML(yamlData, sourceURL, skillDir)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("parse %s: %v", entry.Path, err))
			continue
		}

		// 抓取同目录下的其他文件
		sk.Files = make(map[string]string)
		dirPrefix := skillDir + "/"
		if skillDir == "." {
			dirPrefix = "" // 根目录时不加前缀
		}
		for _, f := range tree.Tree {
			if f.Type != "blob" {
				continue
			}
			// 判断是否在同一目录下（不含子目录的子目录）
			var relPath string
			if dirPrefix == "" {
				// 根目录：只取不含 "/" 的文件（排除子目录文件）
				if strings.Contains(f.Path, "/") {
					continue
				}
				relPath = f.Path
			} else {
				if !strings.HasPrefix(f.Path, dirPrefix) {
					continue
				}
				relPath = strings.TrimPrefix(f.Path, dirPrefix)
			}
			if relPath == "skill.yaml" || relPath == "skill.yml" || relPath == "" {
				continue
			}
			fileURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, f.Path)
			fileData, err := ri.httpGet(fileURL)
			if err != nil {
				continue
			}
			if len(fileData) <= 512*1024 { // 最大 512KB
				sk.Files[relPath] = string(fileData)
			}
		}

		result.Skills = append(result.Skills, *sk)
	}

	if len(result.Skills) == 0 && len(result.Errors) == 0 {
		return nil, fmt.Errorf("no skill.yaml found in repo %s/%s (branch: %s, subpath: %q)", owner, repo, branch, subPath)
	}
	return result, nil
}

// importFromRawURL 直接抓取一个 URL 作为 skill.yaml 解析。
func (ri *RemoteImporter) importFromRawURL(rawURL string) (*ImportResult, error) {
	data, err := ri.httpGet(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	sk, err := ri.parseSkillYAML(data, rawURL, "")
	if err != nil {
		return nil, fmt.Errorf("parse skill.yaml: %w", err)
	}
	return &ImportResult{Skills: []HubSkillFull{*sk}}, nil
}

// parseSkillYAML 解析 skill.yaml 内容为 HubSkillFull。
func (ri *RemoteImporter) parseSkillYAML(data []byte, sourceURL, skillDir string) (*HubSkillFull, error) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("empty YAML document")
	}

	name := strVal(raw, "name")
	if name == "" {
		if skillDir != "" && skillDir != "." {
			name = path.Base(skillDir)
		} else {
			name = "imported-skill"
		}
	}

	version := strVal(raw, "version")
	if version == "" {
		version = "1"
	}

	now := time.Now().Format(time.RFC3339)

	full := &HubSkillFull{
		HubSkillMeta: HubSkillMeta{
			ID:          generateImportID(),
			Name:        name,
			Description: strVal(raw, "description"),
			Tags:        strSlice(raw, "tags"),
			Version:     version,
			Author:      strVal(raw, "author"),
			TrustLevel:  "community",
			CreatedAt:   now,
			UpdatedAt:   now,
			Visible:     true,
			Price:       0,
			Status:      "published",
			Platforms:   strSlice(raw, "platforms"),
			Permissions: strSlice(raw, "permissions"),
			SourceURL:   sourceURL,
		},
		Triggers: strSlice(raw, "triggers"),
		Steps:    parseSteps(raw),
	}
	return full, nil
}

// ── YAML 解析辅助函数 ──────────────────────────────────────────────────

func strVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func strSlice(m map[string]interface{}, key string) []string {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var result []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func parseSteps(m map[string]interface{}) []HubSkillStep {
	raw, ok := m["steps"]
	if !ok {
		return nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var steps []HubSkillStep
	for _, item := range list {
		sm, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		step := HubSkillStep{
			Action:  strVal(sm, "action"),
			OnError: strVal(sm, "on_error"),
		}
		if params, ok := sm["params"].(map[string]interface{}); ok {
			step.Params = params
		}
		steps = append(steps, step)
	}
	return steps
}

// ── HTTP 和 ID 生成 ────────────────────────────────────────────────────

func (ri *RemoteImporter) httpGet(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw-SkillImporter/1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := ri.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5MB limit
}

func generateImportID() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("imp-%d-%s", time.Now().UnixMilli(), hex.EncodeToString(buf[:]))
}

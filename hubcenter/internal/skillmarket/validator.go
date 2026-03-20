package skillmarket

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValidationError 描述一个验证错误。
type ValidationError struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

func (e ValidationError) String() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Message)
}

// ValidationResult 是包验证的汇总结果。
type ValidationResult struct {
	Valid    bool              `json:"valid"`
	Errors   []ValidationError `json:"errors,omitempty"`
	Metadata *SkillMetadata    `json:"metadata,omitempty"` // 解析成功时填充
}

// ValidatePackage 验证解压后的 Skill 包目录。
// 检查 skill.yaml 存在性、YAML 语法、元数据必填字段、脚本语法。
func ValidatePackage(sandboxDir string) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// 1. 检查 skill.yaml 存在
	yamlPath := filepath.Join(sandboxDir, "skill.yaml")
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			File:    "skill.yaml",
			Message: "skill.yaml not found in package root",
		})
		return result, nil
	}

	// 2. 验证 YAML 语法并解析元数据
	yamlErrs := ValidateYAML(yamlPath)
	if len(yamlErrs) > 0 {
		result.Valid = false
		result.Errors = append(result.Errors, yamlErrs...)
		return result, nil
	}

	// 3. 解析元数据并检查必填字段
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("read skill.yaml: %w", err)
	}
	meta, err := ParseSkillYAML(data)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			File:    "skill.yaml",
			Message: err.Error(),
		})
		return result, nil
	}
	result.Metadata = meta

	if metaErrs := ValidateMetadata(meta); len(metaErrs) > 0 {
		result.Valid = false
		for _, msg := range metaErrs {
			result.Errors = append(result.Errors, ValidationError{
				File:    "skill.yaml",
				Message: msg,
			})
		}
	}

	// 4. 扫描并验证脚本文件
	err = filepath.Walk(sandboxDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, _ := filepath.Rel(sandboxDir, path)
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".py":
			if errs := ValidatePython(path); len(errs) > 0 {
				result.Valid = false
				for i := range errs {
					errs[i].File = rel
				}
				result.Errors = append(result.Errors, errs...)
			}
		case ".sh", ".bash":
			if errs := ValidateShell(path); len(errs) > 0 {
				result.Valid = false
				for i := range errs {
					errs[i].File = rel
				}
				result.Errors = append(result.Errors, errs...)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk sandbox: %w", err)
	}

	return result, nil
}

// ValidateYAML 验证 YAML 文件语法。
func ValidateYAML(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if err != nil {
		return []ValidationError{{
			File:    filepath.Base(path),
			Message: fmt.Sprintf("cannot read file: %v", err),
		}}
	}
	var doc any
	if err := yamlUnmarshal(data, &doc); err != nil {
		return []ValidationError{{
			File:    filepath.Base(path),
			Message: fmt.Sprintf("YAML syntax error: %v", err),
		}}
	}
	return nil
}

// yamlUnmarshal 是对 yaml.Unmarshal 的间接引用，方便测试替换。
var yamlUnmarshal = yamlUnmarshalDefault

func yamlUnmarshalDefault(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

// ValidatePython 使用 py_compile 验证 Python 文件语法。
// 如果 python3 不可用，跳过验证。
func ValidatePython(path string) []ValidationError {
	pythonBin := findPython()
	if pythonBin == "" {
		return nil // python 不可用，跳过
	}
	cmd := exec.Command(pythonBin, "-m", "py_compile", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = fmt.Sprintf("py_compile failed: %v", err)
		}
		return []ValidationError{{
			File:    filepath.Base(path),
			Message: msg,
		}}
	}
	return nil
}

// ValidateShell 使用 bash -n 验证 Shell 脚本语法。
// 如果 bash 不可用，跳过验证。
func ValidateShell(path string) []ValidationError {
	bashBin, err := exec.LookPath("bash")
	if err != nil {
		return nil // bash 不可用，跳过
	}
	cmd := exec.Command(bashBin, "-n", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = fmt.Sprintf("bash -n failed: %v", err)
		}
		return []ValidationError{{
			File:    filepath.Base(path),
			Message: msg,
		}}
	}
	return nil
}

// findPython 查找可用的 Python 解释器。
func findPython() string {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

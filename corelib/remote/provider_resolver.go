package remote

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ProviderResolveResult 服务商解析结果。
type ProviderResolveResult struct {
	Provider     corelib.ModelConfig // 最终选中的服务商
	Fallback     bool                // 是否发生了降级
	OriginalName string              // 原始目标服务商名称（降级时有值）
	Reason       string              // 选择原因描述
	Tried        []string            // 已尝试的服务商名称列表
	Errors       []string            // 各服务商的失败原因
}

// ProviderResolver 服务商解析器。
type ProviderResolver struct{}

// Resolve 解析服务商，支持三种模式：
// 1. providerOverride 非空 → 直接使用指定服务商（不降级）
// 2. providerOverride 为空 → 使用 CurrentModel 默认服务商
// 3. 默认服务商不可用 → 按 Models 列表顺序降级
func (r *ProviderResolver) Resolve(toolCfg corelib.ToolConfig, providerOverride string) (ProviderResolveResult, error) {
	if len(toolCfg.Models) == 0 {
		return ProviderResolveResult{}, fmt.Errorf("没有可用的服务商配置")
	}

	override := strings.TrimSpace(providerOverride)

	if override != "" {
		return r.resolveExplicit(toolCfg, override)
	}

	return r.resolveAuto(toolCfg)
}

func (r *ProviderResolver) resolveExplicit(toolCfg corelib.ToolConfig, name string) (ProviderResolveResult, error) {
	for _, m := range toolCfg.Models {
		if strings.EqualFold(m.ModelName, name) {
			if IsValidProvider(m) {
				return ProviderResolveResult{
					Provider: m,
					Reason:   fmt.Sprintf("使用用户指定的服务商: %s", m.ModelName),
				}, nil
			}
			available := AvailableProviderNames(toolCfg)
			return ProviderResolveResult{}, fmt.Errorf(
				"服务商 %s 未配置 API Key，请先配置。可用服务商: %s",
				m.ModelName, strings.Join(available, ", "),
			)
		}
	}

	all := AllProviderNames(toolCfg)
	return ProviderResolveResult{}, fmt.Errorf(
		"服务商 %s 不存在。可用服务商: %s",
		name, strings.Join(all, ", "),
	)
}

func (r *ProviderResolver) resolveAuto(toolCfg corelib.ToolConfig) (ProviderResolveResult, error) {
	var tried []string
	var errors []string
	defaultName := strings.TrimSpace(toolCfg.CurrentModel)
	defaultFound := false

	if defaultName != "" {
		for _, m := range toolCfg.Models {
			if strings.EqualFold(m.ModelName, defaultName) {
				defaultFound = true
				tried = append(tried, m.ModelName)
				if IsValidProvider(m) {
					return ProviderResolveResult{
						Provider: m,
						Reason:   fmt.Sprintf("使用默认服务商: %s", m.ModelName),
						Tried:    tried,
					}, nil
				}
				errors = append(errors, fmt.Sprintf("%s: 未配置 API Key", m.ModelName))
				break
			}
		}
	}

	for _, m := range toolCfg.Models {
		if defaultFound && strings.EqualFold(m.ModelName, defaultName) {
			continue
		}
		tried = append(tried, m.ModelName)
		if IsValidProvider(m) {
			isFallback := defaultName != ""
			originalName := defaultName
			reason := fmt.Sprintf("使用第一个可用服务商: %s", m.ModelName)
			if defaultName != "" {
				reason = fmt.Sprintf("默认服务商 %s 不可用，已降级到 %s", defaultName, m.ModelName)
			}
			return ProviderResolveResult{
				Provider:     m,
				Fallback:     isFallback,
				OriginalName: originalName,
				Reason:       reason,
				Tried:        tried,
				Errors:       errors,
			}, nil
		}
		errors = append(errors, fmt.Sprintf("%s: 未配置 API Key", m.ModelName))
	}

	return ProviderResolveResult{
		Tried:  tried,
		Errors: errors,
	}, fmt.Errorf(
		"所有服务商均不可用。已尝试: %s。失败原因: %s",
		strings.Join(tried, ", "), strings.Join(errors, "; "),
	)
}

// IsValidProvider checks if a provider is usable (has API key, is builtin, or has subscription).
func IsValidProvider(m corelib.ModelConfig) bool {
	if m.IsBuiltin || m.HasSubscription {
		return true
	}
	return strings.TrimSpace(m.ApiKey) != ""
}

// AllProviderNames returns all provider names from the ToolConfig.
func AllProviderNames(tc corelib.ToolConfig) []string {
	names := make([]string, len(tc.Models))
	for i, m := range tc.Models {
		names[i] = m.ModelName
	}
	return names
}

// AvailableProviderNames returns names of valid (usable) providers.
func AvailableProviderNames(tc corelib.ToolConfig) []string {
	var names []string
	for _, m := range tc.Models {
		if IsValidProvider(m) {
			names = append(names, m.ModelName)
		}
	}
	return names
}

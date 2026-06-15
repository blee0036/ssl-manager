package service

import (
	"fmt"
	"log"
	"regexp"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// 截断常量
const (
	MaxCommandOutputs  = 20
	MaxStdoutLen       = 32 * 1024 // 32 KB
	MaxStderrLen       = 32 * 1024 // 32 KB
	MaxErrorMessageLen = 8 * 1024  // 8 KB
	MaxCommandLen      = 4 * 1024  // 4 KB
	TruncateMarker     = "...[truncated]"
	MaxAlertSummaryLen = 256
)

// Sanitizer 日志脱敏处理器
type Sanitizer struct {
	patterns []*regexp.Regexp
	replacer string // "[REDACTED]"
}

// NewSanitizer 创建脱敏器，预编译所有正则。
// 如果任何正则编译失败，返回 error（调用方应让服务启动失败）。
func NewSanitizer() (*Sanitizer, error) {
	patterns := []string{
		`Bearer\s+[A-Za-z0-9._\-]+`,
		`-----BEGIN\s+.*?PRIVATE KEY-----[\s\S]*?-----END\s+.*?PRIVATE KEY-----`,
		`-----BEGIN\s+.*?PRIVATE KEY-----[\s\S]*$`,
		`(?i)(?:KEY|SECRET|TOKEN|PASSWORD)\s*[=:]\s*\S+`,
	}

	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("sanitizer regex compile failed: %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}

	return &Sanitizer{patterns: compiled, replacer: "[REDACTED]"}, nil
}

// Sanitize 对单个字符串执行脱敏。
// 运行时如遇 panic → fail-closed，返回 [REDACTED]。
func (s *Sanitizer) Sanitize(input string) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = "[REDACTED]"
			log.Printf("sanitizer panic (fail-closed): %v", r)
		}
	}()

	result = input
	for _, re := range s.patterns {
		result = re.ReplaceAllString(result, s.replacer)
	}
	return result
}

// TruncateField 截断字段到 maxLen，超限时追加 TruncateMarker。
// marker 不计入 maxLen。
func TruncateField(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + TruncateMarker
}

// SanitizeDeploymentLog 对整个 DeploymentLog 执行：脱敏 → 截断 → 再脱敏。
// 处理顺序：
//  1. 第一次脱敏所有文本字段
//  2. 截断字段到限制长度
//  3. 截断 command_outputs 数组到 MaxCommandOutputs 条
//  4. 第二次脱敏（防止截断破坏 PEM 块结构导致正则不匹配）
func (s *Sanitizer) SanitizeDeploymentLog(dl *model.DeploymentLog) {
	// 第一次脱敏
	s.sanitizeAllFields(dl)

	// 字段截断
	s.truncateAllFields(dl)

	// command_outputs 数组截断到 20 条
	if len(dl.CommandOutputs) > MaxCommandOutputs {
		dl.CommandOutputs = dl.CommandOutputs[:MaxCommandOutputs]
	}

	// 第二次脱敏（防止截断暴露半截 PEM）
	s.sanitizeAllFields(dl)
}

// sanitizeAllFields 对 DeploymentLog 中所有文本字段执行脱敏
func (s *Sanitizer) sanitizeAllFields(dl *model.DeploymentLog) {
	dl.ErrorMessage = s.Sanitize(dl.ErrorMessage)
	for i := range dl.CommandOutputs {
		dl.CommandOutputs[i].Command = s.Sanitize(dl.CommandOutputs[i].Command)
		dl.CommandOutputs[i].Stdout = s.Sanitize(dl.CommandOutputs[i].Stdout)
		dl.CommandOutputs[i].Stderr = s.Sanitize(dl.CommandOutputs[i].Stderr)
	}
}

// truncateAllFields 对 DeploymentLog 中所有文本字段执行截断
func (s *Sanitizer) truncateAllFields(dl *model.DeploymentLog) {
	dl.ErrorMessage = TruncateField(dl.ErrorMessage, MaxErrorMessageLen)
	for i := range dl.CommandOutputs {
		dl.CommandOutputs[i].Command = TruncateField(dl.CommandOutputs[i].Command, MaxCommandLen)
		dl.CommandOutputs[i].Stdout = TruncateField(dl.CommandOutputs[i].Stdout, MaxStdoutLen)
		dl.CommandOutputs[i].Stderr = TruncateField(dl.CommandOutputs[i].Stderr, MaxStderrLen)
	}
}

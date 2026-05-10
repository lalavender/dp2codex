package protocol

import "strings"

var defaultModelMapping = map[string]string{
	"gpt-5.5":           "deepseek-v4-pro",
	"gpt-5":             "deepseek-v4-pro",
	"deepseek-chat":     "deepseek-v4-pro",
	"deepseek-v4-pro":   "deepseek-v4-pro",
	"deepseek-v4-flash": "deepseek-v4-flash",
}

// MapModel 模型名称映射
func MapModel(name string, customMapping map[string]any) string {
	if name == "" {
		return "deepseek-v4-pro"
	}
	// 先查自定义映射
	if customMapping != nil {
		if v, ok := customMapping[name]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	// 再查默认映射
	if v, ok := defaultModelMapping[name]; ok {
		return v
	}
	// 如果已经是 DeepSeek 模型，直接返回
	if strings.HasPrefix(name, "deepseek-") {
		return name
	}
	return "deepseek-v4-pro"
}

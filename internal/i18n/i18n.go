// Package i18n provides UI string lookup for en/zh (logs stay English).
// i18n 包提供界面文案 en/zh 查找（日志保持英文，不走本包）。
package i18n

import (
	"strings"
	"sync"
)

// Language codes supported for UI text.
// 界面文案支持的语言代码。
const (
	LangEN = "en"
	LangZH = "zh"
)

var (
	mu   sync.RWMutex
	lang = LangEN
)

// SetLang sets the active UI language (en or zh). Unknown values fall back to en.
// SetLang 设置界面语言（en 或 zh）；未知值回退为 en。
func SetLang(code string) {
	mu.Lock()
	defer mu.Unlock()
	switch strings.ToLower(strings.TrimSpace(code)) {
	case LangZH, "zh-cn", "zh_cn", "chinese", "cn":
		lang = LangZH
	default:
		lang = LangEN
	}
}

// Lang returns the active language code (en or zh).
// Lang 返回当前语言代码（en 或 zh）。
func Lang() string {
	mu.RLock()
	defer mu.RUnlock()
	return lang
}

// T returns the translated string for key in the active language.
// Missing keys fall back to English, then to the key itself.
// T 返回当前语言下 key 的译文；缺失时回退英文，再回退 key 本身。
func T(key string) string {
	mu.RLock()
	l := lang
	mu.RUnlock()
	if l == LangZH {
		if s, ok := zh[key]; ok {
			return s
		}
	}
	if s, ok := en[key]; ok {
		return s
	}
	return key
}

// Tf is T with simple {name} placeholder replacement from kv pairs.
// Tf 在 T 基础上用 kv 对替换 {name} 占位符。
func Tf(key string, kv map[string]string) string {
	s := T(key)
	for k, v := range kv {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

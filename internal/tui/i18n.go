package tui

import (
	"os"
	"strings"
)

// LocaleID 是 TUI 层语言标识。当前国际化只服务 UI，不暴露给 core/daemon。
type LocaleID string

const (
	LocaleEN LocaleID = "en"
	LocaleZH LocaleID = "zh"
)

type translator struct {
	locale LocaleID
	keys   map[string]map[LocaleID]string
}

func newTranslator(locale LocaleID) *translator {
	return &translator{locale: locale, keys: defaultTranslationKeys()}
}

func (t *translator) T(key string) string {
	if translations, ok := t.keys[key]; ok {
		if text, ok := translations[t.locale]; ok && text != "" {
			return text
		}
		if text, ok := translations[LocaleEN]; ok && text != "" {
			return text
		}
	}
	return key
}

func (t *translator) Tf(key string, args ...any) string {
	text := t.T(key)
	if len(args) == 0 {
		return text
	}
	return strings.ReplaceAll(text, "{}", strings.Trim(strings.Join(strings.Fields(fmtSprintf(args...)), " "), "[]"))
}

func (t *translator) SetLocale(locale LocaleID) {
	t.locale = locale
}

func (t *translator) Locale() LocaleID {
	return t.locale
}

func (t *translator) LoadLocale(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		translations := strings.SplitN(parts[1], "|", 2)
		if t.keys[key] == nil {
			t.keys[key] = make(map[LocaleID]string)
		}
		t.keys[key][LocaleEN] = translations[0]
		if len(translations) > 1 {
			t.keys[key][LocaleZH] = translations[1]
		}
	}
	return nil
}

func (t *TUI) tr(key string, args ...any) string {
	if t == nil || t.i18n == nil {
		return key
	}
	if len(args) == 0 {
		return t.i18n.T(key)
	}
	return t.i18n.Tf(key, args...)
}

func fmtSprintf(args ...any) string {
	var parts []string
	for _, a := range args {
		parts = append(parts, anyToString(a))
	}
	return strings.Join(parts, " ")
}

func anyToString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return intToStr(x)
	case int64:
		return int64ToStr(x)
	case float64:
		return float64ToStr(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := 20
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func int64ToStr(i int64) string     { return intToStr(int(i)) }
func float64ToStr(f float64) string { return intToStr(int(f)) }

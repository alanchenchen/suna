package guard

import (
	"net"
	"net/url"
	"strings"
)

func assessFilesystemRisk(params map[string]any) RiskLevel {
	action, _ := params["action"].(string)
	path, _ := params["path"].(string)
	dst, _ := params["destination"].(string)
	if isHighRiskFilePath(path) || (dst != "" && isHighRiskFilePath(dst)) {
		return RiskHigh
	}
	if action == "copy" {
		if sensitive, _ := IsSensitivePath(path); sensitive {
			return RiskHigh
		}
	}
	switch action {
	case "stat":
		return RiskLow
	case "remove":
		if recursive, _ := params["recursive"].(bool); recursive {
			return RiskHigh
		}
		return RiskMedium
	case "mkdir", "move", "copy":
		return RiskMedium
	default:
		return RiskMedium
	}
}

func assessSearchRisk(params map[string]any) RiskLevel {
	path, _ := params["path"].(string)
	mode, _ := params["mode"].(string)
	if sensitive, _ := IsSensitivePath(path); sensitive {
		return RiskHigh
	}
	if mode == "name" {
		return RiskLow
	}
	if broadSearchPath(path) {
		return RiskMedium
	}
	if useDefault, ok := params["use_default_exclude"].(bool); ok && !useDefault && broadSearchPath(path) {
		return RiskMedium
	}
	return RiskLow
}

func assessHTTPRisk(params map[string]any) RiskLevel {
	method, _ := params["method"].(string)
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	risk := RiskLow
	switch method {
	case "DELETE":
		risk = RiskHigh
	case "POST", "PUT", "PATCH":
		risk = RiskMedium
	}
	urlStr, _ := params["url"].(string)
	if isPrivateURL(urlStr) && risk == RiskLow {
		return RiskMedium
	}
	return risk
}

func isPrivateURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

func broadSearchPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	return trimmed == "" || trimmed == "/" || trimmed == "~"
}

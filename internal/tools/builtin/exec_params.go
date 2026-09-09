package builtin

import (
	"fmt"
	"math"
)

// validateExecParams 仅接受 JSON 解码后的明确类型，不猜测或转换参数值。
// 所有组合检查均在创建进程或访问后台注册表之前完成。
func validateExecParams(params map[string]any) error {
	for name, value := range params {
		switch name {
		case "action", "command", "cwd", "scope", "shell", "job_id", "intent":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%s must be a string", name)
			}
		case "background":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("background must be a boolean")
			}
		case "timeout":
			if _, _, err := parseExecTimeout(params); err != nil {
				return err
			}
		case "cursor":
			number, ok := value.(float64)
			// 上界必须严格小于 2^63，避免 float64 舍入后的 int64 溢出。
			if !ok || math.IsNaN(number) || number < 0 || number >= 0x1p63 || math.Trunc(number) != number {
				return fmt.Errorf("cursor must be a non-negative integer within int64 range")
			}
		case "env":
			values, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("env must be an object of strings")
			}
			for _, item := range values {
				if _, ok := item.(string); !ok {
					return fmt.Errorf("env values must be strings")
				}
			}
		default:
			return fmt.Errorf("unknown exec parameter: %s", name)
		}
	}
	action := "run"
	if value, exists := params["action"]; exists {
		action = value.(string)
	}
	if action != "run" && action != "status" && action != "stop" {
		return fmt.Errorf("action must be run, status, or stop")
	}
	for name := range params {
		allowed := name == "action" || name == "intent"
		if action == "run" {
			allowed = allowed || name != "job_id" && name != "cursor"
		} else {
			allowed = allowed || name == "job_id" || action == "status" && name == "cursor"
		}
		if !allowed {
			return fmt.Errorf("%s is not valid for action=%s", name, action)
		}
	}
	if action != "run" {
		if id, _ := params["job_id"].(string); id == "" {
			return fmt.Errorf("job_id is required")
		}
		return nil
	}
	if command, _ := params["command"].(string); command == "" {
		return fmt.Errorf("command is required")
	}
	if scope, exists := params["scope"]; exists {
		if background, _ := params["background"].(bool); !background {
			return fmt.Errorf("scope is only valid for background runs")
		}
		if scope != execScopeRun && scope != execScopeSession {
			return fmt.Errorf("scope must be run or session")
		}
	}
	if shell, exists := params["shell"]; exists {
		if shell != "auto" && shell != "bash" && shell != "powershell" && shell != "cmd" {
			return fmt.Errorf("shell must be auto, bash, powershell, or cmd")
		}
	}
	return nil
}

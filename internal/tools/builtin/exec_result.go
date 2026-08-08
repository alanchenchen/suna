package builtin

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/alanchenchen/suna/internal/tools"
)

func makeExecResult(action, scope, jobID, status string, output []byte, isError bool, errorText string, truncated bool, extra map[string]any) tools.Result {
	var content strings.Builder
	readableStatus := strings.ReplaceAll(status, "_", " ")
	// 先按错误事实组织文案，避免无效请求被误述为进程状态变化。
	if isError {
		switch status {
		case execStatusNotFound:
			if jobID != "" {
				fmt.Fprintf(&content, "Exec %s request could not find job %s.", action, jobID)
			} else {
				fmt.Fprintf(&content, "Exec %s request is invalid because no job ID was provided.", action)
			}
		case execStatusAccessDenied:
			if jobID != "" {
				fmt.Fprintf(&content, "Exec %s request was denied access to job %s.", action, jobID)
			} else {
				fmt.Fprintf(&content, "Exec %s request was denied.", action)
			}
		default:
			if jobID != "" {
				fmt.Fprintf(&content, "Exec %s request for job %s failed.", action, jobID)
			} else {
				fmt.Fprintf(&content, "Exec %s request failed with status %s.", action, readableStatus)
			}
		}
	} else {
		switch action {
		case "run":
			if jobID != "" && status == execStatusRunning {
				fmt.Fprintf(&content, "Exec started background job %s.", jobID)
			} else {
				fmt.Fprintf(&content, "Exec command completed with status %s.", readableStatus)
			}
		case "status":
			if status == execStatusRunning {
				fmt.Fprintf(&content, "Exec job %s is running.", jobID)
			} else {
				fmt.Fprintf(&content, "Exec job %s finished with status %s.", jobID, readableStatus)
			}
		case "stop":
			fmt.Fprintf(&content, "Exec stop request for job %s completed. Current status: %s.", jobID, readableStatus)
		default:
			fmt.Fprintf(&content, "Exec request completed with status %s.", readableStatus)
		}
	}

	var details []string
	if scope != "" {
		details = append(details, fmt.Sprintf("Scope: %s.", scope))
	}
	if value, ok := extra["exit_code"]; ok {
		details = append(details, fmt.Sprintf("Exit code: %v.", value))
	}
	if value, ok := extra["duration_ms"]; ok {
		details = append(details, fmt.Sprintf("Elapsed: %v ms.", value))
	}
	if value, ok := extra["timeout_seconds"]; ok {
		details = append(details, fmt.Sprintf("Timeout: %v seconds.", value))
	}
	if value, ok := extra["next_cursor"]; ok {
		details = append(details, fmt.Sprintf("Next output cursor: %v.", value))
	}
	if value, ok := extra["cleanup_status"]; ok {
		details = append(details, fmt.Sprintf("Cleanup: %v.", value))
	}
	if truncated {
		details = append(details, "Output truncated: true.")
	}
	if action == "run" && jobID != "" && status == execStatusRunning {
		details = append(details, "Use exec action status with this job ID to read output, or action stop to stop it.")
	} else if action == "status" && jobID != "" && status == execStatusRunning {
		details = append(details, "Use the next output cursor on the next status call, or use action stop to stop the job.")
	}
	if len(details) > 0 {
		content.WriteByte('\n')
		content.WriteString(strings.Join(details, " "))
	}
	if errorText != "" {
		content.WriteString("\nError: ")
		content.WriteString(execReadableError(errorText))
	}
	if len(output) > 0 {
		content.WriteByte('\n')
		content.Write(output)
	}

	metadata := map[string]any{"kind": "exec", "action": action, "exec_status": status, "output_truncated": truncated}
	if jobID != "" {
		metadata["job_id"] = jobID
	}
	if scope != "" {
		metadata["scope"] = scope
	}
	for key, value := range extra {
		metadata[key] = value
	}
	return tools.Result{Content: content.String(), Error: errorText, IsError: isError, Truncated: truncated, Metadata: metadata}
}

// execReadableError 把诊断压成单行，避免控制字符破坏可读状态与输出边界。
func execReadableError(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

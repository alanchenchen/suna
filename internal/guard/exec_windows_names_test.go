package guard

import "testing"

// windowsAnalyze 是平台无关的 Windows 保守解析，POSIX 测试直接覆盖，
// 确保 Windows 行为不依赖 CI 才能验证。
func TestWindowsAnalyzeExtractsCommandsAndRedirects(t *testing.T) {
	cases := []struct {
		name    string
		command string
		names   []string
	}{
		{"simple", "dir", []string{"dir"}},
		{"rm rf", "rm -rf /", []string{"rm"}},
		{"compound and", "ls && rm -rf important", []string{"ls", "rm"}},
		{"powershell semicolon", "Get-ChildItem; Remove-Item -Recurse -Force C:\\Users\\me", []string{"Get-ChildItem", "Remove-Item"}},
		{"cmd pipe", "dir | findstr modified", []string{"dir", "findstr"}},
		{"quoted name", `"C:\Program Files\git\bin\git.exe" status`, []string{`C:\Program Files\git\bin\git.exe`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := windowsAnalyze(tc.command)
			got := make([]string, 0, len(a.Commands))
			for _, c := range a.Commands {
				got = append(got, c.Name)
			}
			if len(got) != len(tc.names) {
				t.Fatalf("commands = %v, want %v", got, tc.names)
			}
			for i := range got {
				if got[i] != tc.names[i] {
					t.Fatalf("commands = %v, want %v", got, tc.names)
				}
			}
		})
	}
}

// windowsAnalyze 不产生空 Name 记录（路径作为命令参数捕获，不单独提取），
// 避免污染 risk 判断（dir C:\Windows 应保持只读 low 而非 medium）。
func TestWindowsAnalyzeNoEmptyNameRecords(t *testing.T) {
	a := windowsAnalyze(`dir C:\Windows`)
	for _, c := range a.Commands {
		if c.Name == "" {
			t.Fatalf("empty-name record found: %v", c)
		}
	}
}

// windowsAnalyze 提取的命令名能驱动结构性高危判断（rm -rf / 拦截）。
func TestWindowsAnalyzeDrivesStructuralRisk(t *testing.T) {
	a := windowsAnalyze("rm -rf /")
	if !execStructurallyHighRisk(&a) {
		t.Fatalf("rm -rf / structural risk = false, want true")
	}
}

// windowsAnalyze 的重定向提取：$null 与 NUL 豁免（丢弃输出，无文件副作用）。
func TestWindowsAnalyzeRedirectExemptions(t *testing.T) {
	a := windowsAnalyze(`echo hi 2>$null > NUL`)
	for _, r := range a.Redirects {
		if r.Target != "" {
			t.Fatalf("redirect target = %q, want empty (exempted)", r.Target)
		}
	}
}

//go:build !windows

package guard

import (
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// posixAnalyzer 用 mvdan.cc/sh 的完整 shell AST 解析命令。
// 相比旧的正则/分词器：命令名精确（引号内文本不是命令）、重定向精确（Op/fd 区分）、
// 动态表达式可见（CmdSubst 节点可递归解析内部命令）。
type posixAnalyzer struct{}

func (posixAnalyzer) Analyze(command, shell string) (ExecAnalysis, bool) {
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		// 语法错误：无法可靠解析，调用方保守处理（安全方向）。
		return ExecAnalysis{ParseFailed: true}, false
	}
	var analysis ExecAnalysis
	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.CallExpr:
			analysis.Commands = append(analysis.Commands, execCommandFromCall(n))
		case *syntax.Redirect:
			analysis.Redirects = append(analysis.Redirects, execRedirectFromNode(n))
		case *syntax.BinaryCmd:
			// 管道链（|）收集命令名顺序；&& / || 不是管道，跳过。
			if n.Op == syntax.Pipe {
				analysis.Pipelines = append(analysis.Pipelines, collectPipelineNames(n))
			}
		case *syntax.CmdSubst:
			// 动态表达式：提取内部命令名后不再深入遍历（内部 CallExpr 由 DynamicCmds 单独处理，
			// 避免重复出现在 Commands 里导致只读判断误判）。
			analysis.HasDynamic = true
			analysis.DynamicCmds = append(analysis.DynamicCmds, extractCmdSubstNames(n)...)
			return false
		}
		return true
	})
	return analysis, true
}

// collectPipelineNames 收集管道链的命令名（递归 BinaryCmd 两侧）。
// curl -s x | tee f | sh → ["curl", "tee", "sh"]。
func collectPipelineNames(bin *syntax.BinaryCmd) []string {
	var names []string
	names = append(names, pipelineSideNames(bin.X)...)
	names = append(names, pipelineSideNames(bin.Y)...)
	return names
}

// pipelineSideNames 从管道一侧的 Stmt 提取命令名；嵌套管道递归。
func pipelineSideNames(stmt *syntax.Stmt) []string {
	if stmt == nil || stmt.Cmd == nil {
		return nil
	}
	switch c := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		if len(c.Args) > 0 {
			if name := wordLiteral(c.Args[0]); name != "" {
				return []string{name}
			}
		}
	case *syntax.BinaryCmd:
		if c.Op == syntax.Pipe {
			return collectPipelineNames(c)
		}
	}
	return nil
}

// newExecAnalyzer 是唯一的平台判断点（POSIX 侧）。
func newExecAnalyzer() ExecAnalyzer {
	return posixAnalyzer{}
}

// execCommandFromCall 从 AST CallExpr 提取命令名与参数。
// Args[0] 是命令名（引号内文本是 Word 参数，不是命令）。
func execCommandFromCall(call *syntax.CallExpr) ExecCommand {
	cmd := ExecCommand{}
	if len(call.Args) > 0 {
		cmd.Name = wordLiteral(call.Args[0])
	}
	// 赋值语句（x=$(...)）可能被解析为无 Args 的 CallExpr，需防越界。
	if len(call.Args) > 1 {
		for _, arg := range call.Args[1:] {
			if lit := wordLiteral(arg); lit != "" {
				cmd.Args = append(cmd.Args, lit)
			}
		}
	}
	return cmd
}

// execRedirectFromNode 从 AST Redirect 提取目标、操作与 fd。
// Op 区分写/追加/读/fd，fd 重定向（2>&1）不参与 workspace 路径检查。
func execRedirectFromNode(redir *syntax.Redirect) ExecRedirect {
	r := ExecRedirect{FD: 0}
	switch redir.Op {
	case syntax.RdrOut, syntax.RdrAll, syntax.RdrClob:
		r.Op = "write"
	case syntax.AppOut, syntax.AppAll, syntax.AppClob:
		r.Op = "append"
	case syntax.RdrIn, syntax.RdrInOut:
		r.Op = "read"
	case syntax.DplIn, syntax.DplOut:
		// fd 复制（2>&1、<&0）：目标是 fd 号，不参与 workspace 路径检查。
		r.Op = "fd"
	default:
		r.Op = "write"
	}
	if redir.N != nil {
		// N 是 fd 字面量（如 2> 的 "2"），也可能是 {varname}（Bash 扩展），非数字时按 0 处理。
		if fd, err := strconv.Atoi(redir.N.Value); err == nil {
			r.FD = fd
		}
	}
	if redir.Word != nil {
		r.Target = wordLiteral(redir.Word)
	}
	return r
}

// extractCmdSubstNames 递归提取命令替换内部的所有命令名。
// $(git rev-parse HEAD) → ["git"]；$(curl x | sh) → ["curl", "sh"]。
func extractCmdSubstNames(subst *syntax.CmdSubst) []string {
	var names []string
	for _, stmt := range subst.Stmts {
		if stmt.Cmd == nil {
			continue
		}
		names = append(names, commandNames(stmt.Cmd)...)
	}
	return names
}

// commandNames 从命令节点提取命令名；管道（BinaryCmd Pipe）递归两侧。
func commandNames(cmd syntax.Command) []string {
	switch c := cmd.(type) {
	case *syntax.CallExpr:
		if len(c.Args) > 0 {
			if name := wordLiteral(c.Args[0]); name != "" {
				return []string{name}
			}
		}
	case *syntax.BinaryCmd:
		// 管道（|）与 && / || 都递归两侧，提取所有命令名。
		var names []string
		names = append(names, stmtCommandNames(c.X)...)
		names = append(names, stmtCommandNames(c.Y)...)
		return names
	case *syntax.Subshell:
		for _, stmt := range c.Stmts {
			if stmt.Cmd != nil {
				names := commandNames(stmt.Cmd)
				if len(names) > 0 {
					return names
				}
			}
		}
	}
	return nil
}

// stmtCommandNames 从 Stmt 提取命令名（BinaryCmd 的 X/Y 是 Stmt 而非 Command）。
func stmtCommandNames(stmt *syntax.Stmt) []string {
	if stmt == nil || stmt.Cmd == nil {
		return nil
	}
	return commandNames(stmt.Cmd)
}

// wordLiteral 返回 Word 的字面量表示。
// 变量展开（$dir）保留变量名形式（"$dir"），命令替换（$(pwd)）保留 "$()" 形式，
// 使 workspace 检查能识别动态部分（isPathCandidate 排除含 $ 的参数，与旧正则语义一致）。
func wordLiteral(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var sb strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			sb.WriteString(p.Value)
		case *syntax.SglQuoted:
			sb.WriteString(p.Value)
		case *syntax.ParamExp:
			sb.WriteString("$" + p.Param.Value)
		case *syntax.CmdSubst:
			sb.WriteString("$()")
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				switch inner := dp.(type) {
				case *syntax.Lit:
					sb.WriteString(inner.Value)
				case *syntax.ParamExp:
					sb.WriteString("$" + inner.Param.Value)
				case *syntax.CmdSubst:
					sb.WriteString("$()")
				}
			}
		}
	}
	return sb.String()
}

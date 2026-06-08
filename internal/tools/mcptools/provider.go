package mcptools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/mcp"
	"github.com/alanchenchen/suna/internal/tools"
)

const prefix = "mcp__"

type Provider struct {
	runtime       *mcp.Runtime
	attachmentDir string
	mu            sync.RWMutex
	nameMap       map[string]toolRef
}

type toolRef struct {
	Server string
	Tool   string
}

func NewProvider(runtime *mcp.Runtime, attachmentDir string) *Provider {
	return &Provider{runtime: runtime, attachmentDir: attachmentDir, nameMap: map[string]toolRef{}}
}

func (p *Provider) Specs(ctx context.Context) ([]tools.Spec, error) {
	if p == nil || p.runtime == nil {
		return nil, nil
	}
	items, err := p.runtime.Tools(ctx)
	if err != nil {
		return nil, err
	}
	specs := make([]tools.Spec, 0, len(items))
	nameMap := make(map[string]toolRef, len(items))
	for _, item := range items {
		params := item.InputSchema
		if len(params) == 0 {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		publicName := PublicName(item.Server, item.Name)
		nameMap[publicName] = toolRef{Server: item.Server, Tool: item.Name}
		specs = append(specs, tools.Spec{
			Name:        publicName,
			Description: item.Description,
			Parameters:  params,
			Category:    tools.Act,
			Source:      tools.Source{Kind: tools.SourceMCP, ID: item.Server},
			Guard:       tools.GuardNever,
			Metadata:    map[string]any{"mcp_tool": item.Name},
		})
	}
	p.mu.Lock()
	p.nameMap = nameMap
	p.mu.Unlock()
	return specs, nil
}

func (p *Provider) Execute(ctx context.Context, call tools.Call) (tools.Result, bool) {
	ref, ok := p.lookup(call.Name)
	if !ok {
		return tools.Result{}, false
	}
	res, err := p.runtime.CallTool(ctx, ref.Server, ref.Tool, call.Params)
	if err != nil {
		return tools.ErrorResult(err.Error()), true
	}
	content := p.formatResult(ref.Server, ref.Tool, res)
	if res.IsError {
		return tools.Result{Content: content, Error: content, IsError: true}, true
	}
	return tools.TextResult(content), true
}

func (p *Provider) Close(ctx context.Context) error {
	if p == nil || p.runtime == nil {
		return nil
	}
	return p.runtime.Close(ctx)
}

func (p *Provider) lookup(publicName string) (toolRef, bool) {
	p.mu.RLock()
	ref, ok := p.nameMap[publicName]
	p.mu.RUnlock()
	if ok {
		return ref, true
	}
	server, rawTool, ok := ParsePublicName(publicName)
	if !ok {
		return toolRef{}, false
	}
	return toolRef{Server: server, Tool: rawTool}, true
}

func PublicName(server, tool string) string {
	return prefix + sanitizeName(server) + "__" + sanitizeName(tool)
}

func ParsePublicName(name string) (server string, tool string, ok bool) {
	if !strings.HasPrefix(name, prefix) {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(name, prefix), "__", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	s = unsafeNameChars.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		return "unnamed"
	}
	return s
}

func (p *Provider) formatResult(server, toolName string, res mcp.CallResult) string {
	var parts []string
	for _, item := range res.Content {
		switch item.Type {
		case "", "text":
			if strings.TrimSpace(item.Text) != "" {
				parts = append(parts, item.Text)
			}
		case "image":
			parts = append(parts, p.saveBinaryContent(server, toolName, item, "image"))
		default:
			if item.Data != "" {
				parts = append(parts, p.saveBinaryContent(server, toolName, item, item.Type))
			} else if item.Text != "" {
				parts = append(parts, item.Text)
			} else {
				parts = append(parts, fmt.Sprintf("[MCP %s content omitted]", item.Type))
			}
		}
	}
	if len(parts) == 0 {
		return "[MCP tool returned no content]"
	}
	return strings.Join(parts, "\n")
}

func (p *Provider) saveBinaryContent(server, toolName string, item mcp.Content, kind string) string {
	if p.attachmentDir == "" || item.Data == "" {
		return fmt.Sprintf("[MCP %s content omitted: %s]", kind, item.MimeType)
	}
	data, err := base64.StdEncoding.DecodeString(item.Data)
	if err != nil {
		return fmt.Sprintf("[MCP %s content decode failed: %v]", kind, err)
	}
	if err := os.MkdirAll(p.attachmentDir, 0755); err != nil {
		return fmt.Sprintf("[MCP %s content save failed: %v]", kind, err)
	}
	name := item.Name
	if name == "" {
		name = fmt.Sprintf("mcp-%s-%s-%d-%s%s", sanitizeName(server), sanitizeName(toolName), time.Now().UnixNano(), uuid.New().String()[:8], extFromMime(item.MimeType))
	}
	path := filepath.Join(p.attachmentDir, filepath.Base(name))
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Sprintf("[MCP %s content save failed: %v]", kind, err)
	}
	return fmt.Sprintf("[MCP %s content saved: %s (%s, %d bytes)]", kind, path, item.MimeType, len(data))
}

func extFromMime(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	case "application/json":
		return ".json"
	case "text/css":
		return ".css"
	case "text/plain":
		return ".txt"
	default:
		return ".bin"
	}
}

var _ tools.Provider = (*Provider)(nil)

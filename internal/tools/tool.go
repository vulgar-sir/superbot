package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"superbot/internal/llm"
)

// Result is what a tool returns to the model. Errors are represented as
// IsError results so the model can recover instead of aborting the loop.
type Result struct {
	Content      string
	IsError      bool
	ArtifactPath string
	// WidgetHTML carries an MCP App UI resource. It is rendered in a sandboxed
	// iframe by the client and never enters the model context.
	WidgetHTML string
}

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

type Registry struct {
	tools map[string]Tool
	order []string
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(tool Tool) {
	name := tool.Name()
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = tool
}

func (r *Registry) Definitions() []llm.ToolDef {
	sort.Strings(r.order)
	out := make([]llm.ToolDef, 0, len(r.order))
	for _, name := range r.order {
		tool := r.tools[name]
		out = append(out, llm.ToolDef{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		})
	}
	return out
}

// Has reports whether a tool with this name is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// similar lists registered tools whose name contains the unknown name, or
// is contained in it: a model that calls "write" means workspace_write.
func (r *Registry) similar(name string) []string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return nil
	}
	var out []string
	for _, candidate := range r.order {
		if strings.Contains(candidate, lower) || strings.Contains(lower, candidate) {
			out = append(out, candidate)
		}
	}
	sort.Strings(out)
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) Result {
	tool, ok := r.tools[name]
	if !ok {
		message := fmt.Sprintf("unknown tool %q", name)
		if similar := r.similar(name); len(similar) > 0 {
			message += "；可用的相近工具：" + strings.Join(similar, ", ")
		}
		return Result{Content: message, IsError: true}
	}
	result, err := tool.Execute(ctx, args)
	if err != nil {
		return Result{Content: err.Error(), IsError: true, ArtifactPath: result.ArtifactPath}
	}
	return result
}

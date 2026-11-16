package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

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

func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) Result {
	tool, ok := r.tools[name]
	if !ok {
		return Result{Content: fmt.Sprintf("unknown tool %q", name), IsError: true}
	}
	result, err := tool.Execute(ctx, args)
	if err != nil {
		return Result{Content: err.Error(), IsError: true, ArtifactPath: result.ArtifactPath}
	}
	return result
}

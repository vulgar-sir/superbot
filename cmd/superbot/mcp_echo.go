package main

import (
	"context"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// runMCPEcho is a tiny stdio MCP server used to verify the stdio transport
// end-to-end without installing Node/Python in the worker image.
func runMCPEcho() error {
	server := mcp.NewServer(&mcp.Implementation{Name: "superbot-echo", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "echo",
		Description: "Echo the given text back.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		Text string `json:"text"`
	}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{
			&mcp.TextContent{Text: "echo: " + args.Text},
		}}, nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "echo_widget",
		Description: "Return a small MCP App widget that displays the given text.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct {
		Text string `json:"text"`
	}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "widget: " + args.Text}},
			Meta: mcp.Meta{"ui": map[string]any{
				"resourceUri": "ui://echo/widget",
			}},
		}, nil, nil
	})
	server.AddResource(&mcp.Resource{
		URI:      "ui://echo/widget",
		Name:     "echo-widget",
		MIMEType: "text/html",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      "ui://echo/widget",
			MIMEType: "text/html",
			Text:     `<!doctype html><html><body style="font:14px sans-serif;padding:16px"><h3>Echo MCP App</h3><pre id="out"></pre><script>document.getElementById("out").textContent = String(window.__MCP_TOOL_RESULT__ || "");</script></body></html>`,
		}}}, nil
	})
	return server.Run(context.Background(), &mcp.StdioTransport{})
}

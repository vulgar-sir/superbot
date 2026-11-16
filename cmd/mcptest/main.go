package main

import (
	"context"
	"log"
	"net/http"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoArgs struct {
	Text string `json:"text" jsonschema:"text to echo back"`
}

// mcptest is a tiny development MCP server used to verify the HTTP MCP
// integration end to end.
func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "superbot-test-mcp", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "echo",
		Description: "Echo the provided text back to the caller.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in echoArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + in.Text}},
		}, nil, nil
	})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	log.Println("mcptest listening on 127.0.0.1:9099/mcp")
	log.Fatal(http.ListenAndServe("127.0.0.1:9099", mux))
}

package mcpservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"superbot/internal/secrets"
	"superbot/internal/store"
	"superbot/internal/tools"
)

const (
	maxToolResult  = 32 * 1024
	connectTimeout = 20 * time.Second
)

type Service struct {
	db     *store.DB
	box    *secrets.Box
	logger *slog.Logger
}

func NewService(db *store.DB, box *secrets.Box, logger *slog.Logger) *Service {
	return &Service{db: db, box: box, logger: logger}
}

type Toolset struct {
	sessions []*mcp.ClientSession
	tools    []tools.Tool
	logger   *slog.Logger
}

func (t *Toolset) Tools() []tools.Tool { return t.tools }

func (t *Toolset) Close() {
	for _, session := range t.sessions {
		if err := session.Close(); err != nil {
			t.logger.Warn("close mcp session failed", "error", err)
		}
	}
}

func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, name, serverURL string, headers map[string]string, createdBy string) (store.McpServer, error) {
	name = strings.TrimSpace(name)
	serverURL = strings.TrimSpace(serverURL)
	if name == "" {
		return store.McpServer{}, fmt.Errorf("name is required")
	}
	parsed, err := url.Parse(serverURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return store.McpServer{}, fmt.Errorf("url must be a valid http(s) URL")
	}
	status := "active"
	if createdBy == "bot" {
		status = "draft"
	}
	var headersEnc []byte
	if len(headers) > 0 {
		raw, _ := json.Marshal(headers)
		headersEnc, err = s.box.Encrypt(raw)
		if err != nil {
			return store.McpServer{}, fmt.Errorf("encrypt headers: %w", err)
		}
	}
	return s.db.CreateMcpServer(ctx, store.CreateMcpServerParams{
		OwnerID:    ownerID,
		Name:       name,
		Transport:  "http",
		Command:    "",
		Args:       json.RawMessage(`[]`),
		EnvEnc:     nil,
		Url:        serverURL,
		HeadersEnc: headersEnc,
		Status:     status,
		CreatedBy:  createdBy,
		Config:     json.RawMessage(`{}`),
	})
}

// CreateStdio registers a command-based MCP server. The command runs on the
// worker host (admin-controlled, no shell interpolation) and speaks MCP over
// stdin/stdout.
func (s *Service) CreateStdio(ctx context.Context, ownerID uuid.UUID, name, command string, args []string, env map[string]string, createdBy string) (store.McpServer, error) {
	name = strings.TrimSpace(name)
	command = strings.TrimSpace(command)
	if name == "" || command == "" {
		return store.McpServer{}, fmt.Errorf("name and command are required")
	}
	argsRaw, _ := json.Marshal(args)
	var envEnc []byte
	var err error
	if len(env) > 0 {
		raw, _ := json.Marshal(env)
		envEnc, err = s.box.Encrypt(raw)
		if err != nil {
			return store.McpServer{}, fmt.Errorf("encrypt env: %w", err)
		}
	}
	status := "active"
	if createdBy == "bot" {
		status = "draft"
	}
	return s.db.CreateMcpServer(ctx, store.CreateMcpServerParams{
		OwnerID:    ownerID,
		Name:       name,
		Transport:  "stdio",
		Command:    command,
		Args:       argsRaw,
		EnvEnc:     envEnc,
		Url:        "",
		HeadersEnc: nil,
		Status:     status,
		CreatedBy:  createdBy,
		Config:     json.RawMessage(`{}`),
	})
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]store.McpServer, error) {
	return s.db.ListMcpServers(ctx, ownerID)
}

func (s *Service) SetStatus(ctx context.Context, ownerID, serverID uuid.UUID, status string) (store.McpServer, error) {
	return s.db.SetMcpServerStatus(ctx, store.SetMcpServerStatusParams{ID: serverID, OwnerID: ownerID, Status: status})
}

func (s *Service) Delete(ctx context.Context, ownerID, serverID uuid.UUID) error {
	n, err := s.db.DeleteMcpServer(ctx, store.DeleteMcpServerParams{ID: serverID, OwnerID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("mcp server not found")
	}
	return nil
}

func (s *Service) SetBotServers(ctx context.Context, ownerID, botID uuid.UUID, serverIDs []uuid.UUID) error {
	if _, err := s.db.GetBot(ctx, store.GetBotParams{ID: botID, OwnerID: ownerID}); err != nil {
		return fmt.Errorf("bot not found")
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.db.WithTx(tx)
	if err := q.ClearBotMcpServers(ctx, botID); err != nil {
		return err
	}
	seen := make(map[uuid.UUID]bool)
	for _, serverID := range serverIDs {
		if seen[serverID] {
			continue
		}
		seen[serverID] = true
		if _, err := q.GetMcpServer(ctx, store.GetMcpServerParams{ID: serverID, OwnerID: ownerID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("mcp server not found")
			}
			return err
		}
		if err := q.UpsertBotMcpServer(ctx, store.UpsertBotMcpServerParams{BotID: botID, McpServerID: serverID}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) Test(ctx context.Context, ownerID, serverID uuid.UUID) ([]string, error) {
	server, err := s.db.GetMcpServer(ctx, store.GetMcpServerParams{ID: serverID, OwnerID: ownerID})
	if err != nil {
		return nil, fmt.Errorf("mcp server not found")
	}
	session, err := s.connect(ctx, server)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	names := make([]string, 0, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		names = append(names, tool.Name)
	}
	return names, nil
}

func (s *Service) LoadToolset(ctx context.Context, ownerID, botID uuid.UUID) (*Toolset, error) {
	servers, err := s.db.ListEnabledMcpServers(ctx, botID)
	if err != nil {
		return nil, err
	}
	set := &Toolset{logger: s.logger}
	usedNames := make(map[string]bool)
	for _, server := range servers {
		if server.OwnerID != ownerID {
			continue
		}
		connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
		session, err := s.connect(connectCtx, server)
		cancel()
		if err != nil {
			s.logger.Warn("mcp server connection failed", "server", server.Name, "error", err)
			continue
		}
		listCtx, listCancel := context.WithTimeout(ctx, connectTimeout)
		toolsResult, err := session.ListTools(listCtx, nil)
		listCancel()
		if err != nil {
			s.logger.Warn("mcp list tools failed", "server", server.Name, "error", err)
			_ = session.Close()
			continue
		}
		set.sessions = append(set.sessions, session)
		for _, tool := range toolsResult.Tools {
			name := mcpToolName(server.Name, tool.Name, usedNames)
			schema := json.RawMessage(`{"type":"object","properties":{}}`)
			if tool.InputSchema != nil {
				if raw, err := json.Marshal(tool.InputSchema); err == nil && json.Valid(raw) {
					schema = raw
				}
			}
			set.tools = append(set.tools, &MCPTool{
				session:     session,
				logger:      set.logger,
				displayName: name,
				description: fmt.Sprintf("[%s] %s", server.Name, tool.Description),
				schema:      schema,
				remoteName:  tool.Name,
			})
		}
	}
	return set, nil
}

type MCPTool struct {
	session     *mcp.ClientSession
	logger      *slog.Logger
	displayName string
	description string
	schema      json.RawMessage
	remoteName  string
}

func (t *MCPTool) Name() string                { return t.displayName }
func (t *MCPTool) Description() string         { return t.description }
func (t *MCPTool) Parameters() json.RawMessage { return t.schema }

func (t *MCPTool) Execute(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	args := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return tools.Result{}, fmt.Errorf("tool arguments must be a JSON object: %w", err)
		}
	}
	result, err := t.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      t.remoteName,
		Arguments: args,
	})
	if err != nil {
		return tools.Result{}, fmt.Errorf("mcp call %s: %w", t.remoteName, err)
	}
	var b strings.Builder
	for _, content := range result.Content {
		switch value := content.(type) {
		case *mcp.TextContent:
			b.WriteString(value.Text)
		default:
			raw, err := json.Marshal(content)
			if err == nil {
				b.WriteString(string(raw))
			}
		}
	}
	if b.Len() == 0 && result.StructuredContent != nil {
		if raw, err := json.Marshal(result.StructuredContent); err == nil {
			b.WriteString(string(raw))
		}
	}
	content := b.String()
	if content == "" {
		content = "(empty result)"
	}
	if len(content) > maxToolResult {
		content = content[:maxToolResult] + "\n[truncated]"
	}
	widget := ""
	if uri := mcpWidgetURI(result.Meta); uri != "" {
		t.logger.Info("mcp widget detected", "tool", t.remoteName, "uri", uri)
		resource, readErr := t.session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if readErr != nil {
			t.logger.Warn("mcp widget resource failed", "uri", uri, "error", readErr)
		} else {
			for _, item := range resource.Contents {
				if item == nil || item.Text == "" {
					continue
				}
				widget = item.Text
				if len(widget) > 512*1024 {
					widget = widget[:512*1024]
				}
				break
			}
		}
	}
	return tools.Result{Content: content, IsError: result.IsError, WidgetHTML: widget}, nil
}

// mcpWidgetURI extracts the MCP Apps resource URI from a tool result.
func mcpWidgetURI(meta mcp.Meta) string {
	if meta == nil {
		return ""
	}
	if value, ok := meta["ui"].(map[string]any); ok {
		if uri, ok := value["resourceUri"].(string); ok {
			return strings.TrimSpace(uri)
		}
	}
	if uri, ok := meta["ui/resourceUri"].(string); ok {
		return strings.TrimSpace(uri)
	}
	return ""
}

func (s *Service) connect(ctx context.Context, server store.McpServer) (*mcp.ClientSession, error) {
	clientObj := mcp.NewClient(&mcp.Implementation{Name: "superbot", Version: "0.1.0"}, nil)
	if server.Transport == "stdio" {
		args := []string{}
		if len(server.Args) > 0 {
			_ = json.Unmarshal(server.Args, &args)
		}
		cmd := exec.Command(server.Command, args...)
		configured := map[string]string{}
		if len(server.EnvEnc) > 0 {
			raw, err := s.box.Decrypt(server.EnvEnc)
			if err != nil {
				return nil, fmt.Errorf("decrypt mcp env: %w", err)
			}
			if err := json.Unmarshal(raw, &configured); err != nil {
				return nil, fmt.Errorf("decode mcp env: %w", err)
			}
		}
		cmd.Env = buildChildEnv(os.Environ(), configured)
		session, err := clientObj.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			return nil, fmt.Errorf("connect stdio mcp server %q: %w", server.Name, err)
		}
		return session, nil
	}
	if server.Transport != "http" {
		return nil, fmt.Errorf("unsupported MCP transport %q", server.Transport)
	}
	headers := map[string]string{}
	if len(server.HeadersEnc) > 0 {
		raw, err := s.box.Decrypt(server.HeadersEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt mcp headers: %w", err)
		}
		if err := json.Unmarshal(raw, &headers); err != nil {
			return nil, fmt.Errorf("decode mcp headers: %w", err)
		}
	}
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{Transport: &headerTransport{base: baseTransport, headers: headers}}
	session, err := clientObj.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             server.Url,
		HTTPClient:           client,
		MaxRetries:           1,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect mcp server %q: %w", server.Name, err)
	}
	return session, nil
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for key, value := range h.headers {
		clone.Header.Set(key, value)
	}
	return h.base.RoundTrip(clone)
}

var unsafeToolChars = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func mcpToolName(server, tool string, used map[string]bool) string {
	serverPart := strings.Trim(unsafeToolChars.ReplaceAllString(server, "_"), "_")
	toolPart := strings.Trim(unsafeToolChars.ReplaceAllString(tool, "_"), "_")
	if serverPart == "" {
		serverPart = "server"
	}
	if toolPart == "" {
		toolPart = "tool"
	}
	base := "mcp__" + serverPart + "__" + toolPart
	if len(base) > 60 {
		base = base[:60]
	}
	name := base
	for i := 2; used[name]; i++ {
		suffix := fmt.Sprintf("_%d", i)
		name = base
		if len(name)+len(suffix) > 64 {
			name = name[:64-len(suffix)]
		}
		name += suffix
	}
	used[name] = true
	return name
}

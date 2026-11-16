package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/connectors"
)

type connectorEndpoint struct {
	base       string
	authHeader string
	authPrefix string
	extra      map[string]string
}

var connectorEndpoints = map[string]connectorEndpoint{
	"github":       {base: "https://api.github.com", authHeader: "Authorization", authPrefix: "Bearer ", extra: map[string]string{"Accept": "application/vnd.github+json"}},
	"slack":        {base: "https://slack.com/api", authHeader: "Authorization", authPrefix: "Bearer "},
	"notion":       {base: "https://api.notion.com", authHeader: "Authorization", authPrefix: "Bearer ", extra: map[string]string{"Notion-Version": "2022-06-28"}},
	"linear":       {base: "https://api.linear.app", authHeader: "Authorization"},
	"figma":        {base: "https://api.figma.com", authHeader: "X-Figma-Token"},
	"vercel":       {base: "https://api.vercel.com", authHeader: "Authorization", authPrefix: "Bearer "},
	"sentry":       {base: "https://sentry.io/api/0", authHeader: "Authorization", authPrefix: "Bearer "},
	"atlassian":    {base: "https://api.atlassian.com", authHeader: "Authorization", authPrefix: "Bearer "},
	"google_docs":  {base: "https://www.googleapis.com", authHeader: "Authorization", authPrefix: "Bearer "},
	"google_drive": {base: "https://www.googleapis.com", authHeader: "Authorization", authPrefix: "Bearer "},
	"canva":        {base: "https://api.canva.com/rest/v1", authHeader: "Authorization", authPrefix: "Bearer "},
	"onedrive":     {base: "https://graph.microsoft.com/v1.0", authHeader: "Authorization", authPrefix: "Bearer "},
	"msteams":      {base: "https://graph.microsoft.com/v1.0", authHeader: "Authorization", authPrefix: "Bearer "},
}

type connectorRequestTool struct {
	connectors *connectors.Service
	ownerID    uuid.UUID
	client     *http.Client
}

func NewConnectorRequestTool(service *connectors.Service, ownerID uuid.UUID) Tool {
	return &connectorRequestTool{
		connectors: service,
		ownerID:    ownerID,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *connectorRequestTool) Name() string { return "connector_request" }
func (t *connectorRequestTool) Description() string {
	return "调用用户已连接应用的官方 API（Linear/Figma/Vercel/Sentry/Atlassian/Google Docs/Google Drive/Canva/OneDrive/Microsoft Teams/GitHub/Slack/Notion）。provider 选应用，path 以 / 开头，会自动附加已保存的凭据。"
}
func (t *connectorRequestTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"provider":{"type":"string","enum":["github","slack","notion","linear","figma","vercel","sentry","atlassian","google_docs","google_drive","canva","onedrive","msteams"]},"method":{"type":"string","enum":["GET","POST","PUT","PATCH","DELETE"]},"path":{"type":"string"},"body":{"type":"object"},"headers":{"type":"object"}},"required":["provider","path"]}`)
}
func (t *connectorRequestTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Provider string            `json:"provider"`
		Method   string            `json:"method"`
		Path     string            `json:"path"`
		Body     json.RawMessage   `json:"body"`
		Headers  map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	endpoint, ok := connectorEndpoints[provider]
	if !ok {
		return Result{Content: "该 Provider 不支持通用请求，或尚未连接。", IsError: true}, nil
	}
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return Result{Content: "method 不受支持", IsError: true}, nil
	}
	path := strings.TrimSpace(input.Path)
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") || strings.Contains(path, "://") {
		return Result{Content: "path 必须以 / 开头且只能指向该应用的 API", IsError: true}, nil
	}
	connector, err := t.connectors.ByProvider(ctx, t.ownerID, provider)
	if err != nil {
		return Result{Content: "尚未连接 " + provider + "，请先在连接应用中添加凭据。", IsError: true}, nil
	}
	token, err := t.connectors.AccessToken(connector)
	if err != nil {
		return Result{Content: "凭据不可用: " + err.Error(), IsError: true}, nil
	}
	var body io.Reader
	if len(input.Body) > 0 && string(input.Body) != "null" {
		body = bytes.NewReader(input.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.base+path, body)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	req.Header.Set(endpoint.authHeader, endpoint.authPrefix+token)
	for key, value := range endpoint.extra {
		req.Header.Set(key, value)
	}
	for key, value := range input.Headers {
		lower := strings.ToLower(key)
		if lower == "authorization" || lower == "x-figma-token" || lower == "host" || lower == "cookie" {
			continue
		}
		req.Header.Set(key, value)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return Result{Content: "请求失败: " + err.Error(), IsError: true}, nil
	}
	defer resp.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	text := strings.TrimSpace(string(content))
	if text == "" {
		text = "(empty)"
	}
	return Result{
		Content: fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, text),
		IsError: resp.StatusCode >= 300,
	}, nil
}

type postgresQueryTool struct {
	connectors *connectors.Service
	ownerID    uuid.UUID
}

func NewPostgresQueryTool(service *connectors.Service, ownerID uuid.UUID) Tool {
	return &postgresQueryTool{connectors: service, ownerID: ownerID}
}

func (t *postgresQueryTool) Name() string { return "postgres_query" }
func (t *postgresQueryTool) Description() string {
	return "在用户已连接的 Postgres 上执行只读查询（SELECT/WITH/SHOW/EXPLAIN）。凭据在连接应用中保存为 Postgres DSN。"
}
func (t *postgresQueryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"sql":{"type":"string"}},"required":["sql"]}`)
}
func (t *postgresQueryTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		SQL string `json:"sql"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	sql, err := checkReadOnlySQL(input.SQL)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	connector, err := t.connectors.ByProvider(ctx, t.ownerID, "postgres")
	if err != nil {
		return Result{Content: "尚未连接 Postgres，请先在连接应用中保存 DSN。", IsError: true}, nil
	}
	dsn, err := t.connectors.AccessToken(connector)
	if err != nil {
		return Result{Content: "Postgres 凭据不可用: " + err.Error(), IsError: true}, nil
	}
	queryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, err := pgx.Connect(queryCtx, dsn)
	if err != nil {
		return Result{Content: "连接 Postgres 失败: " + err.Error(), IsError: true}, nil
	}
	defer conn.Close(context.WithoutCancel(ctx))
	// The lexical check above is only a fast, friendly rejection. The
	// read-only transaction is what actually stops writes, including ones
	// hidden in functions the lexer cannot see into.
	tx, err := conn.BeginTx(queryCtx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return Result{Content: "开启只读事务失败: " + err.Error(), IsError: true}, nil
	}
	defer func() {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer rollbackCancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if _, err := tx.Exec(queryCtx, postgresStatementTimeoutSQL); err != nil {
		return Result{Content: "设置查询超时失败: " + err.Error(), IsError: true}, nil
	}
	rows, err := tx.Query(queryCtx, sql)
	if err != nil {
		return Result{Content: "查询失败: " + err.Error(), IsError: true}, nil
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	out := make([]map[string]any, 0, 100)
	for len(out) < 100 && rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return Result{Content: "读取结果失败: " + err.Error(), IsError: true}, nil
		}
		item := make(map[string]any, len(fields))
		for index, field := range fields {
			if index < len(values) {
				item[string(field.Name)] = values[index]
			}
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return Result{Content: "查询失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(out)
	return Result{Content: string(encoded)}, nil
}

const postgresStatementTimeoutSQL = "SET LOCAL statement_timeout = '15s'"

var readOnlySQLLeadingWords = map[string]bool{
	"SELECT": true, "WITH": true, "SHOW": true, "EXPLAIN": true,
}

var readOnlySQLDeniedWords = map[string]bool{
	"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true, "TRUNCATE": true,
	"DROP": true, "ALTER": true, "CREATE": true, "GRANT": true, "REVOKE": true,
	"COPY": true, "CALL": true, "DO": true, "VACUUM": true, "REINDEX": true,
	"REFRESH": true, "LOCK": true, "SET": true, "RESET": true,
}

func isSQLWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isSQLWordPart(c byte) bool {
	return isSQLWordStart(c) || (c >= '0' && c <= '9') || c == '$'
}

// checkReadOnlySQL is the lexical pre-check for postgres_query. It accepts a
// single SELECT/WITH/SHOW/EXPLAIN statement and returns it without its
// trailing semicolon. Keywords are only looked for outside string literals,
// quoted identifiers and comments, so `SELECT 'delete me'` and a column named
// "update" pass while a data-modifying CTE or EXPLAIN ANALYZE does not. An
// unquoted identifier that equals a denied keyword (e.g. a column named set)
// is rejected too; quote it.
func checkReadOnlySQL(sql string) (string, error) {
	var (
		errNotReadOnly  = fmt.Errorf("只允许只读查询（SELECT/WITH/SHOW/EXPLAIN）。")
		errMultiple     = fmt.Errorf("一次只能执行一条查询。")
		errUnterminated = fmt.Errorf("SQL 中存在未闭合的字符串、引号或注释。")
	)
	firstWord := ""
	sawToken := false
	hasAnalyze := false
	end := -1 // index of the terminating semicolon, if any
	prevWord, prevWordEnd := "", -1

	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
			continue
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			newline := strings.IndexByte(sql[i:], '\n')
			if newline < 0 {
				i = len(sql)
			} else {
				i += newline + 1
			}
			continue
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			// Postgres block comments nest.
			depth := 1
			i += 2
			for depth > 0 {
				if i+1 >= len(sql) {
					return "", errUnterminated
				}
				switch {
				case sql[i] == '/' && sql[i+1] == '*':
					depth++
					i += 2
				case sql[i] == '*' && sql[i+1] == '/':
					depth--
					i += 2
				default:
					i++
				}
			}
			continue
		}

		// Everything below is real statement content.
		if end >= 0 {
			return "", errMultiple
		}
		switch {
		case c == ';':
			end = i
			i++
		case c == '\'':
			// E'...' strings use backslash escapes; plain strings only ''.
			backslash := prevWordEnd == i && strings.EqualFold(prevWord, "E")
			i++
			closed := false
			for i < len(sql) {
				if backslash && sql[i] == '\\' {
					i += 2
					continue
				}
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return "", errUnterminated
			}
			sawToken = true
		case c == '"':
			i++
			closed := false
			for i < len(sql) {
				if sql[i] == '"' {
					if i+1 < len(sql) && sql[i+1] == '"' {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return "", errUnterminated
			}
			sawToken = true
		case c == '$':
			// $tag$ ... $tag$ is a string literal; $1 is a parameter.
			j := i + 1
			if j < len(sql) && isSQLWordStart(sql[j]) {
				for j < len(sql) && isSQLWordPart(sql[j]) && sql[j] != '$' {
					j++
				}
			}
			if j < len(sql) && sql[j] == '$' {
				tag := sql[i : j+1]
				closing := strings.Index(sql[j+1:], tag)
				if closing < 0 {
					return "", errUnterminated
				}
				i = j + 1 + closing + len(tag)
			} else {
				i++
			}
			sawToken = true
		case isSQLWordStart(c):
			start := i
			for i < len(sql) && isSQLWordPart(sql[i]) {
				i++
			}
			word := strings.ToUpper(sql[start:i])
			prevWord, prevWordEnd = word, i
			if !sawToken {
				firstWord = word
			}
			sawToken = true
			if readOnlySQLDeniedWords[word] {
				return "", errNotReadOnly
			}
			if word == "ANALYZE" || word == "ANALYSE" {
				hasAnalyze = true
			}
		default:
			sawToken = true
			i++
		}
	}

	if !readOnlySQLLeadingWords[firstWord] {
		return "", errNotReadOnly
	}
	// EXPLAIN ANALYZE actually executes the statement it explains.
	if firstWord == "EXPLAIN" && hasAnalyze {
		return "", fmt.Errorf("不允许 EXPLAIN ANALYZE：它会真正执行语句。")
	}
	if end >= 0 {
		sql = sql[:end]
	}
	return strings.TrimSpace(sql), nil
}

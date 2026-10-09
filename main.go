package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// ConfluenceConfig holds the configuration for the Confluence client.
type ConfluenceConfig struct {
	BaseURL string
	Token   string
}

const (
	// defaultLimit is the default number of results for paginated requests.
	defaultLimit = 25
	maxRESTInt   = 1<<31 - 1
)

// loadConfig loads configuration from environment variables.
func loadConfig() (*ConfluenceConfig, error) {
	token := os.Getenv("CONFLUENCE_API_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("CONFLUENCE_API_TOKEN environment variable is required")
	}

	rawURL := os.Getenv("CONFLUENCE_BASE_URL")
	if rawURL == "" {
		rawURL = os.Getenv("CONFLUENCE_API_BASE_PATH")
	}
	if rawURL == "" {
		rawURL = os.Getenv("CONFLUENCE_HOST")
	}

	if rawURL == "" {
		return nil, fmt.Errorf("CONFLUENCE_BASE_URL, CONFLUENCE_API_BASE_PATH or CONFLUENCE_HOST environment variable is required")
	}
	if strings.ContainsAny(rawURL, "?#") {
		return nil, fmt.Errorf("base URL must not contain a query or fragment")
	}

	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("base URL must use http or https scheme")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("base URL must have a hostname")
	}

	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	if !strings.HasSuffix(u.Path, "/rest/api") {
		u = u.JoinPath("rest/api")
	}

	return &ConfluenceConfig{
		BaseURL: u.String(),
		Token:   token,
	}, nil
}

// ConfluenceClient is a client for the Confluence API.
type ConfluenceClient struct {
	config     *ConfluenceConfig
	httpClient *http.Client
}

// NewConfluenceClient creates a new instance of ConfluenceClient with a default timeout.
func NewConfluenceClient(config *ConfluenceConfig) *ConfluenceClient {
	return &ConfluenceClient{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// executeRequest performs an authenticated HTTP request and returns the response.
// The caller is responsible for closing the response body.
func (c *ConfluenceClient) executeRequest(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
	u, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	u = u.JoinPath(path)

	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.config.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// doRequest performs an authenticated HTTP request and returns the body as bytes.
// It handles basic error checking; successful response bytes are returned unchanged.
func (c *ConfluenceClient) doRequest(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	resp, err := c.executeRequest(ctx, method, path, query, body)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}

// getJSON is a helper to perform a GET request and unmarshal the result into a target object efficiently.
func (c *ConfluenceClient) getJSON(ctx context.Context, path string, query url.Values, target any) error {
	resp, err := c.executeRequest(ctx, "GET", path, query, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}
	return nil
}

// SpaceRef represents a reference to a Confluence space in API responses/requests.
type SpaceRef struct {
	Key string `json:"key" `
}

// BodyStorage represents the storage format of the body content.
type BodyStorage struct {
	Value          string `json:"value"`
	Representation string `json:"representation"`
	valuePresent   bool
}

// UnmarshalJSON distinguishes a valid empty body from an absent/null storage value.
func (s *BodyStorage) UnmarshalJSON(data []byte) error {
	var storage struct {
		Value          *string `json:"value"`
		Representation string  `json:"representation"`
	}
	if err := json.Unmarshal(data, &storage); err != nil {
		return err
	}
	s.Value = ""
	s.valuePresent = storage.Value != nil
	if storage.Value != nil {
		s.Value = *storage.Value
	}
	s.Representation = storage.Representation
	return nil
}

// Body represents the body of a Confluence page, typically containing storage format.
type Body struct {
	Storage *BodyStorage `json:"storage,omitempty"`
}

// Version represents the version information of a Confluence page.
type Version struct {
	Number  int    `json:"number"`
	Message string `json:"message,omitempty"`
}

// Ancestor represents an ancestor page of a Confluence page.
type Ancestor struct {
	ID string `json:"id"`
}

// ConfluencePage represents a Confluence page or blogpost structure.
type ConfluencePage struct {
	ID        string     `json:"id,omitempty"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Space     *SpaceRef  `json:"space,omitempty"`
	Body      *Body      `json:"body,omitempty"`
	Version   *Version   `json:"version,omitempty"`
	Ancestors []Ancestor `json:"ancestors,omitempty"`
}

// getArguments helper extracts the "arguments" dictionary from an MCP tool request.
func getArguments(req mcp.CallToolRequest) (map[string]any, error) {
	if req.Params.Arguments == nil {
		return make(map[string]any), nil
	}
	args, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("arguments are not a JSON object")
	}
	return args, nil
}

// ensureExpand adds a property to an expansion string if not already present.
func ensureExpand(current, required string) string {
	if current == "" {
		return required
	}
	parts := strings.Split(current, ",")
	for _, p := range parts {
		if strings.TrimSpace(p) == required {
			return current
		}
	}
	return current + "," + required
}

// readOptionalIntArgument validates MCP JSON numbers before converting to REST ints.
func readOptionalIntArgument(args map[string]any, name string, min int) (int, bool, error) {
	raw, present := args[name]
	if !present {
		return 0, false, nil
	}
	value, ok := raw.(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < float64(min) || value > maxRESTInt {
		return 0, true, fmt.Errorf("%s must be an integer between %d and %d", name, min, maxRESTInt)
	}
	return int(value), true, nil
}

// newPaginatedQuery is only used for the paginated /search endpoint.
func newPaginatedQuery(args map[string]any) (url.Values, error) {
	query := url.Values{}
	limit, present, err := readOptionalIntArgument(args, "limit", 0)
	if err != nil {
		return nil, err
	}
	if !present {
		limit = defaultLimit
	}
	query.Set("limit", fmt.Sprintf("%d", limit))
	start, present, err := readOptionalIntArgument(args, "start", 0)
	if err != nil {
		return nil, err
	}
	if present {
		query.Set("start", fmt.Sprintf("%d", start))
	}
	if expand, ok := args["expand"].(string); ok && expand != "" {
		query.Set("expand", expand)
	}
	return query, nil
}

// handleGetContent returns a tool handler for retrieving Confluence content by ID.
func handleGetContent(client *ConfluenceClient) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := getArguments(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		contentID, ok := args["contentId"].(string)
		if !ok || contentID == "" {
			return mcp.NewToolResultError("contentId must be a string and is required"), nil
		}

		if strings.Contains(contentID, "/") || strings.Contains(contentID, "..") {
			return mcp.NewToolResultError("invalid contentId format"), nil
		}

		expand, _ := args["expand"].(string)
		query := url.Values{"expand": {ensureExpand(expand, "body.storage")}}

		resp, err := client.doRequest(ctx, "GET", "/content/"+contentID, query, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("error getting content: %v", err)), nil
		}

		return mcp.NewToolResultText(string(resp)), nil
	}
}

// handleSearchContent returns a tool handler for searching Confluence content using CQL.
func handleSearchContent(client *ConfluenceClient) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := getArguments(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		cql, ok := args["cql"].(string)
		if !ok || cql == "" {
			return mcp.NewToolResultError("cql must be a string and is required"), nil
		}

		query, err := newPaginatedQuery(args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		query.Set("cql", cql)

		resp, err := client.doRequest(ctx, "GET", "/search", query, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("error searching content: %v", err)), nil
		}

		return mcp.NewToolResultText(string(resp)), nil
	}
}

// handleCreateContent returns a tool handler for creating new content (page or blogpost) in Confluence.
func handleCreateContent(client *ConfluenceClient) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := getArguments(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		title, ok := args["title"].(string)
		if !ok || title == "" {
			return mcp.NewToolResultError("title is required"), nil
		}
		spaceKey, ok := args["spaceKey"].(string)
		if !ok || spaceKey == "" {
			return mcp.NewToolResultError("spaceKey is required"), nil
		}
		contentStr, ok := args["content"].(string)
		if !ok || contentStr == "" {
			return mcp.NewToolResultError("content is required"), nil
		}

		typeStr := "page"
		if rawType, present := args["type"]; present {
			value, ok := rawType.(string)
			if !ok || (value != "" && value != "page" && value != "blogpost") {
				return mcp.NewToolResultError("type must be page or blogpost (empty defaults to page)"), nil
			}
			if value != "" {
				typeStr = value
			}
		}

		parentID, _ := args["parentId"].(string)

		payload := ConfluencePage{
			Type:  typeStr,
			Title: title,
			Space: &SpaceRef{Key: spaceKey},
			Body: &Body{
				Storage: &BodyStorage{
					Value:          contentStr,
					Representation: "storage",
				},
			},
		}

		if parentID != "" {
			payload.Ancestors = []Ancestor{{ID: parentID}}
		}

		resp, err := client.doRequest(ctx, "POST", "/content", nil, payload)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("error creating content: %v", err)), nil
		}

		return mcp.NewToolResultText(string(resp)), nil
	}
}

// handleUpdateContent returns a tool handler for updating existing content in Confluence.
func handleUpdateContent(client *ConfluenceClient) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := getArguments(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		contentID, ok := args["contentId"].(string)
		if !ok || contentID == "" {
			return mcp.NewToolResultError("contentId is required"), nil
		}

		if strings.Contains(contentID, "/") || strings.Contains(contentID, "..") {
			return mcp.NewToolResultError("invalid contentId format"), nil
		}

		newVersion, explicitVersion, err := readOptionalIntArgument(args, "version", 1)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		query := url.Values{"expand": {"body.storage,version,space"}}
		var currentData ConfluencePage
		if err := client.getJSON(ctx, "/content/"+contentID, query, &currentData); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to retrieve current content: %v", err)), nil
		}

		if !explicitVersion {
			if currentData.Version == nil {
				return mcp.NewToolResultError("could not determine current version from API response"), nil
			}
			if currentData.Version.Number < 1 || currentData.Version.Number >= maxRESTInt {
				return mcp.NewToolResultError("current version cannot be incremented within the REST integer range"), nil
			}
			newVersion = currentData.Version.Number + 1
		}

		title, _ := args["title"].(string)
		contentStr, _ := args["content"].(string)
		versionComment, _ := args["versionComment"].(string)
		if currentData.Type == "" || currentData.Space == nil || currentData.Space.Key == "" {
			return mcp.NewToolResultError("current content must include type and space key"), nil
		}
		if title == "" && currentData.Title == "" {
			return mcp.NewToolResultError("current content must include the title being preserved"), nil
		}
		if contentStr == "" {
			bodyBearing := currentData.Type == "page" || currentData.Type == "blogpost" || currentData.Type == "comment"
			if currentData.Body == nil || currentData.Body.Storage == nil {
				if bodyBearing {
					return mcp.NewToolResultError("current content must include the storage body being preserved"), nil
				}
			} else if !currentData.Body.Storage.valuePresent || currentData.Body.Storage.Representation != "storage" {
				return mcp.NewToolResultError("current storage body must include a non-null value and storage representation"), nil
			}
		}

		payload := ConfluencePage{
			ID:    contentID,
			Type:  currentData.Type,
			Space: currentData.Space,
			Version: &Version{
				Number:  newVersion,
				Message: versionComment,
			},
		}

		if title != "" {
			payload.Title = title
		} else {
			payload.Title = currentData.Title
		}

		if contentStr != "" {
			payload.Body = &Body{
				Storage: &BodyStorage{
					Value:          contentStr,
					Representation: "storage",
				},
			}
		} else if currentData.Body != nil {
			payload.Body = currentData.Body
		}

		resp, err := client.doRequest(ctx, "PUT", "/content/"+contentID, nil, payload)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("error updating content: %v", err)), nil
		}

		return mcp.NewToolResultText(string(resp)), nil
	}
}

// escapeCQLString contains a value in a quoted CQL string, retaining text-query semantics.
func escapeCQLString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

// handleListSpaces returns a tool handler for listing/searching Confluence spaces.
func handleListSpaces(client *ConfluenceClient) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := getArguments(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		searchText, _ := args["searchText"].(string)
		var cql string
		if searchText == "" {
			cql = "type=space"
		} else {
			safeSearchText := escapeCQLString(searchText)
			cql = fmt.Sprintf(`type=space AND title ~ "%s"`, safeSearchText)
		}
		query, err := newPaginatedQuery(args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		query.Set("cql", cql)

		resp, err := client.doRequest(ctx, "GET", "/search", query, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("error listing spaces: %v", err)), nil
		}

		return mcp.NewToolResultText(string(resp)), nil
	}
}

// setupServer configures the MCP server and returns it.
func setupServer(client *ConfluenceClient) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		"confluence-7.9.0-go-mcp",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
	)

	s.AddTool(mcp.NewTool("confluence_get_content",
		mcp.WithDescription("Get content by ID from Confluence 7.9.0 Server (selfhosted)"),
		mcp.WithString("contentId", mcp.Required(), mcp.Description("Confluence content ID")),
		mcp.WithString("expand", mcp.Description("Comma-separated list of properties to expand")),
	), handleGetContent(client))

	s.AddTool(mcp.NewTool("confluence_search_content",
		mcp.WithDescription("Search Confluence 7.9.0 Server (selfhosted) using CQL; returns native SearchResult JSON"),
		mcp.WithString("cql", mcp.Required(), mcp.Description("Confluence Query Language (CQL) search string")),
		mcp.WithNumber("limit", mcp.Min(0), mcp.Max(maxRESTInt), mcp.MultipleOf(1), mcp.Description("Maximum number of results (default: 25; zero is passed to Confluence)")),
		mcp.WithNumber("start", mcp.Min(0), mcp.Max(maxRESTInt), mcp.MultipleOf(1), mcp.Description("Zero-based starting index")),
		mcp.WithString("expand", mcp.Description("Native search expansions, e.g. content.body.storage,content.space")),
	), handleSearchContent(client))

	s.AddTool(mcp.NewTool("confluence_create_content",
		mcp.WithDescription("Create a page or blogpost in Confluence 7.9.0 Server (selfhosted)"),
		mcp.WithString("title", mcp.Required(), mcp.Description("The title of the new content")),
		mcp.WithString("spaceKey", mcp.Required(), mcp.Description("The key of the space where content will be created")),
		mcp.WithString("content", mcp.Required(), mcp.Description("The content of the page in Confluence storage format")),
		mcp.WithString("type", mcp.Enum("", "page", "blogpost"), mcp.Description("Content type; omitted or empty defaults to page")),
		mcp.WithString("parentId", mcp.Description("Parent page ID for a child page (optional)")),
	), handleCreateContent(client))

	s.AddTool(mcp.NewTool("confluence_update_content",
		mcp.WithDescription("Update content in Confluence 7.9.0 Server (selfhosted), preserving unchanged fields"),
		mcp.WithString("contentId", mcp.Required(), mcp.Description("The ID of the content to update")),
		mcp.WithNumber("version", mcp.Min(1), mcp.Max(maxRESTInt), mcp.MultipleOf(1), mcp.Description("Target version (optional, defaults to current version + 1)")),
		mcp.WithString("title", mcp.Description("New title; omitted or empty preserves current")),
		mcp.WithString("content", mcp.Description("New storage body; omitted or empty preserves current")),
		mcp.WithString("versionComment", mcp.Description("A comment for the new version")),
	), handleUpdateContent(client))

	s.AddTool(mcp.NewTool("confluence_list_spaces",
		mcp.WithDescription("List/search space titles in Confluence 7.9.0 Server (selfhosted); returns a page of native SearchResult JSON"),
		mcp.WithString("searchText", mcp.Description("CQL text search in space titles; omitted or empty lists one result page")),
		mcp.WithNumber("limit", mcp.Min(0), mcp.Max(maxRESTInt), mcp.MultipleOf(1), mcp.Description("Maximum number of spaces (default: 25; zero is passed to Confluence)")),
		mcp.WithNumber("start", mcp.Min(0), mcp.Max(maxRESTInt), mcp.MultipleOf(1), mcp.Description("Zero-based starting index")),
		mcp.WithString("expand", mcp.Description("Native search expansions, e.g. space.homepage")),
	), handleListSpaces(client))

	return s
}

type serveFunc func(*mcpserver.MCPServer) error

func run(serve serveFunc) error {
	config, err := loadConfig()
	if err != nil {
		return fmt.Errorf("configuration error: %v", err)
	}

	client := NewConfluenceClient(config)
	s := setupServer(client)

	if err := serve(s); err != nil {
		return fmt.Errorf("server error: %v", err)
	}
	return nil
}

func main() {
	if err := run(func(s *mcpserver.MCPServer) error {
		return mcpserver.ServeStdio(s)
	}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// TestLoadConfig tests configuration loading from environment variables.
func TestLoadConfig(t *testing.T) {
	resetConfigEnv(t)
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
		wantURL string
	}{
		{
			name: "valid config with CONFLUENCE_BASE_URL",
			env: map[string]string{
				"CONFLUENCE_API_TOKEN": "test-token",
				"CONFLUENCE_BASE_URL":  "https://example.atlassian.net",
			},
			wantErr: false,
			wantURL: "https://example.atlassian.net/rest/api",
		},
		{
			name: "valid config with CONFLUENCE_HOST",
			env: map[string]string{
				"CONFLUENCE_API_TOKEN": "test-token",
				"CONFLUENCE_HOST":      "example.atlassian.net",
			},
			wantErr: false,
			wantURL: "https://example.atlassian.net/rest/api",
		},
		{
			name: "missing token",
			env: map[string]string{
				"CONFLUENCE_BASE_URL": "https://example.atlassian.net",
			},
			wantErr: true,
		},
		{
			name: "missing URL",
			env: map[string]string{
				"CONFLUENCE_API_TOKEN": "test-token",
			},
			wantErr: true,
		},
		{
			name: "invalid URL format",
			env: map[string]string{
				"CONFLUENCE_API_TOKEN": "test-token",
				"CONFLUENCE_BASE_URL":  "://invalid-url",
			},
			wantErr: true,
		},
		{
			name: "invalid URL scheme",
			env: map[string]string{
				"CONFLUENCE_API_TOKEN": "test-token",
				"CONFLUENCE_BASE_URL":  "ftp://example.com",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			config, err := loadConfig()
			if (err != nil) != tt.wantErr {
				t.Errorf("loadConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && config.BaseURL != tt.wantURL {
				t.Errorf("loadConfig() BaseURL = %v, want %v", config.BaseURL, tt.wantURL)
			}
		})
	}
}

// TestEnsureExpand tests appending expansion properties.
func TestEnsureExpand(t *testing.T) {
	tests := []struct {
		current  string
		required string
		want     string
	}{
		{"", "body.storage", "body.storage"},
		{"version", "body.storage", "version,body.storage"},
		{"body.storage", "body.storage", "body.storage"},
		{"version,body.storage", "body.storage", "version,body.storage"},
	}

	for _, tt := range tests {
		got := ensureExpand(tt.current, tt.required)
		if got != tt.want {
			t.Errorf("ensureExpand(%q, %q) = %q, want %q", tt.current, tt.required, got, tt.want)
		}
	}
}

// TestGetArguments tests extracting arguments from MCP requests.
func TestGetArguments(t *testing.T) {
	t.Run("nil arguments", func(t *testing.T) {
		req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: nil}}
		args, err := getArguments(req)
		if err != nil || len(args) != 0 {
			t.Errorf("expected empty args, got %v, %v", args, err)
		}
	})

	t.Run("invalid arguments type", func(t *testing.T) {
		req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: "not-a-map"}}
		_, err := getArguments(req)
		if err == nil {
			t.Error("expected error for non-map arguments")
		}
	})
}

// TestNewPaginatedQuery tests mapping MCP arguments to URL query parameters.
func TestNewPaginatedQuery(t *testing.T) {
	args := map[string]any{
		"limit":  float64(10),
		"start":  float64(5),
		"expand": "body.storage",
	}
	query, err := newPaginatedQuery(args)
	if err != nil {
		t.Fatal(err)
	}

	if query.Get("limit") != "10" {
		t.Errorf("expected limit 10, got %s", query.Get("limit"))
	}
	if query.Get("start") != "5" {
		t.Errorf("expected start 5, got %s", query.Get("start"))
	}
	if query.Get("expand") != "body.storage" {
		t.Errorf("expected expand body.storage, got %s", query.Get("expand"))
	}
}

// TestHandleGetContent tests retrieving Confluence content.
func TestHandleGetContent(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/content/123" {
			t.Errorf("expected path /rest/api/content/123, got %s", r.URL.Path)
		}
		if r.Method != http.MethodGet || !reflect.DeepEqual(r.URL.Query(), url.Values{"expand": {"body.storage"}}) {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"123","title":"Test Page"}`))
	}))
	defer server.Close()

	client := NewConfluenceClient(&ConfluenceConfig{
		BaseURL: server.URL + "/rest/api",
		Token:   "test-token",
	})

	handler := handleGetContent(client)
	ctx := context.Background()
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "confluence_get_content",
			Arguments: map[string]any{
				"contentId": "123",
			},
		},
	}

	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("handler failed: %v", err)
	}

	if result.IsError {
		t.Fatalf("handler returned error: %v", result.Content)
	}

	var page map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &page); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if page["id"] != "123" || page["title"] != "Test Page" {
		t.Errorf("unexpected page content: %v", page)
	}

	t.Run("missing contentId", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "confluence_get_content",
				Arguments: map[string]any{},
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for missing contentId")
		}
	})

	t.Run("invalid contentId format", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "confluence_get_content",
				Arguments: map[string]any{
					"contentId": "../etc/passwd",
				},
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for invalid contentId format")
		}
	})

	t.Run("api error", func(t *testing.T) {
		errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
		}))
		defer errServer.Close()

		errClient := NewConfluenceClient(&ConfluenceConfig{BaseURL: errServer.URL, Token: "token"})
		errHandler := handleGetContent(errClient)
		result, err := errHandler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for API 404")
		}
	})
}

// TestHandleListSpaces tests listing and searching spaces.
func TestHandleListSpaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/search" {
			t.Errorf("expected path /rest/api/search, got %s", r.URL.Path)
		}
		cql := r.URL.Query().Get("cql")
		if !strings.Contains(cql, "type=space") {
			t.Errorf("expected cql to contain type=space, got %s", cql)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	client := NewConfluenceClient(&ConfluenceConfig{
		BaseURL: server.URL + "/rest/api",
		Token:   "test-token",
	})

	handler := handleListSpaces(client)
	ctx := context.Background()

	t.Run("list first page", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "confluence_list_spaces",
				Arguments: map[string]any{},
			},
		}
		result, err := handler(ctx, req)
		if err != nil || result.IsError {
			t.Fatalf("handler failed: %v, %v", err, result)
		}
	})

	t.Run("search spaces", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "confluence_list_spaces",
				Arguments: map[string]any{
					"searchText": "Test",
				},
			},
		}
		result, err := handler(ctx, req)
		if err != nil || result.IsError {
			t.Fatalf("handler failed: %v, %v", err, result)
		}
	})
}

// TestHandleSearchContent tests searching content via CQL.
func TestHandleSearchContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cql := r.URL.Query().Get("cql")
		if cql != "title ~ \"Test\"" {
			t.Errorf("expected cql title ~ \"Test\", got %s", cql)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	client := NewConfluenceClient(&ConfluenceConfig{
		BaseURL: server.URL + "/rest/api",
		Token:   "test-token",
	})

	handler := handleSearchContent(client)
	ctx := context.Background()
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "confluence_search_content",
			Arguments: map[string]any{
				"cql": "title ~ \"Test\"",
			},
		},
	}

	result, err := handler(ctx, req)
	if err != nil || result.IsError {
		t.Fatalf("handler failed: %v, %v", err, result)
	}
}

// TestHandleCreateContent tests creating new content.
func TestHandleCreateContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var page ConfluencePage
		if err := json.NewDecoder(r.Body).Decode(&page); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if page.Title != "New Page" {
			t.Errorf("expected title New Page, got %s", page.Title)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"456","title":"New Page"}`))
	}))
	defer server.Close()

	client := NewConfluenceClient(&ConfluenceConfig{
		BaseURL: server.URL + "/rest/api",
		Token:   "test-token",
	})

	handler := handleCreateContent(client)
	ctx := context.Background()
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "confluence_create_content",
			Arguments: map[string]any{
				"title":    "New Page",
				"spaceKey": "TEST",
				"content":  "<p>Hello</p>",
			},
		},
	}

	result, err := handler(ctx, req)
	if err != nil || result.IsError {
		t.Fatalf("handler failed: %v, %v", err, result)
	}
}

// TestHandleUpdateContent tests updating existing content.
func TestHandleUpdateContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"123","type":"page","title":"Old Title","space":{"key":"TS"},"body":{"storage":{"value":"<p>Old</p>","representation":"storage"}},"version":{"number":1}}`))
			return
		}
		if r.Method == "PUT" {
			var page ConfluencePage
			if err := json.NewDecoder(r.Body).Decode(&page); err != nil {
				t.Errorf("failed to decode request body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if page.Version.Number != 2 {
				t.Errorf("expected version 2, got %d", page.Version.Number)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"123","title":"New Title","version":{"number":2}}`))
			return
		}
	}))
	defer server.Close()

	client := NewConfluenceClient(&ConfluenceConfig{
		BaseURL: server.URL + "/rest/api",
		Token:   "test-token",
	})

	handler := handleUpdateContent(client)
	ctx := context.Background()
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "confluence_update_content",
			Arguments: map[string]any{
				"contentId": "123",
				"title":     "New Title",
			},
		},
	}

	result, err := handler(ctx, req)
	if err != nil || result.IsError {
		t.Fatalf("handler failed: %v, %v", err, result)
	}
}

// TestHandleSearchContentErrors tests error handling in search.
func TestHandleSearchContentErrors(t *testing.T) {
	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
	handler := handleSearchContent(client)
	ctx := context.Background()

	t.Run("missing cql", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "confluence_search_content",
				Arguments: map[string]any{},
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for missing cql")
		}
	})
}

// TestHandleCreateContentErrors tests error handling in create.
func TestHandleCreateContentErrors(t *testing.T) {
	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
	handler := handleCreateContent(client)
	ctx := context.Background()

	t.Run("missing required fields", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "confluence_create_content",
				Arguments: map[string]any{},
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for missing title")
		}
	})
}

// TestHandleUpdateContentErrors tests error handling in update.
func TestHandleUpdateContentErrors(t *testing.T) {
	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
	handler := handleUpdateContent(client)
	ctx := context.Background()

	t.Run("missing contentId", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "confluence_update_content",
				Arguments: map[string]any{},
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for missing contentId")
		}
	})
}

// TestExecuteRequestErrors tests edge cases in request execution.
func TestExecuteRequestErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid base URL in client", func(t *testing.T) {
		client := &ConfluenceClient{
			config: &ConfluenceConfig{BaseURL: "%%"}, // Invalid URL
		}
		_, err := client.executeRequest(ctx, "GET", "/path", nil, nil)
		if err == nil {
			t.Error("expected error for invalid base URL")
		}
	})

	t.Run("invalid body marshal", func(t *testing.T) {
		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
		// Channel cannot be marshaled to JSON
		_, err := client.executeRequest(ctx, "POST", "/path", nil, make(chan int))
		if err == nil {
			t.Error("expected error for unmarshalable body")
		}
	})
}

// TestGetJSONErrors tests error paths in getJSON.
func TestGetJSONErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid json response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid-json}`))
		}))
		defer server.Close()

		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL, Token: "t"})
		var target map[string]any
		err := client.getJSON(ctx, "/", nil, &target)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("api error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`error message`))
		}))
		defer server.Close()

		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL, Token: "t"})
		var target map[string]any
		err := client.getJSON(ctx, "/", nil, &target)
		if err == nil || !strings.Contains(err.Error(), "API error") {
			t.Errorf("expected API error, got %v", err)
		}
	})
}

// TestDoRequestAPIError tests API errors in doRequest.
func TestDoRequestAPIError(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`internal server error`))
	}))
	defer server.Close()

	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL, Token: "t"})
	_, err := client.doRequest(ctx, "GET", "/", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "API error (status 500)") {
		t.Errorf("expected 500 API error, got %v", err)
	}
}

// TestLoadConfigMore covers additional paths in loadConfig.
func TestLoadConfigMore(t *testing.T) {
	resetConfigEnv(t)
	t.Run("valid config with CONFLUENCE_API_BASE_PATH", func(t *testing.T) {
		t.Setenv("CONFLUENCE_API_TOKEN", "test-token")
		t.Setenv("CONFLUENCE_API_BASE_PATH", "https://example.com/wiki")
		config, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if config.BaseURL != "https://example.com/wiki/rest/api" {
			t.Errorf("expected URL with /rest/api, got %s", config.BaseURL)
		}
	})

	t.Run("URL without protocol", func(t *testing.T) {
		t.Setenv("CONFLUENCE_API_TOKEN", "test-token")
		t.Setenv("CONFLUENCE_BASE_URL", "example.com")
		config, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(config.BaseURL, "https://") {
			t.Errorf("expected https prefix, got %s", config.BaseURL)
		}
	})
}

// TestHandleCreateContentMore covers additional paths in handleCreateContent.
func TestHandleCreateContentMore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var page ConfluencePage
		_ = json.NewDecoder(r.Body).Decode(&page)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer server.Close()

	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL, Token: "t"})
	handler := handleCreateContent(client)
	ctx := context.Background()

	t.Run("create with parentId and type", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{
					"title":    "Child Page",
					"spaceKey": "TEST",
					"content":  "content",
					"type":     "page",
					"parentId": "123",
				},
			},
		}
		result, err := handler(ctx, req)
		if err != nil || result.IsError {
			t.Fatalf("handler failed: %v, %v", err, result)
		}
		if !strings.Contains(result.Content[0].(mcp.TextContent).Text, `"type":"page"`) {
			t.Error("expected page type in result")
		}
		if !strings.Contains(result.Content[0].(mcp.TextContent).Text, `"ancestors":[{"id":"123"}]`) {
			t.Error("expected ancestors in result")
		}
	})

	t.Run("getArguments failure", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: "invalid",
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for invalid arguments")
		}
	})
}

// TestHandleUpdateContentMore covers additional paths in handleUpdateContent.
func TestHandleUpdateContentMore(t *testing.T) {
	ctx := context.Background()

	t.Run("update with explicit version and versions comment", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				_, _ = w.Write([]byte(`{"id":"123","type":"page","title":"Old","space":{"key":"TS"},"body":{"storage":{"value":"old content","representation":"storage"}}}`))
				return
			}
			var page ConfluencePage
			_ = json.NewDecoder(r.Body).Decode(&page)
			if page.Version.Number != 10 {
				t.Errorf("expected version 10, got %d", page.Version.Number)
			}
			if page.Version.Message != "update msg" {
				t.Errorf("expected message update msg, got %s", page.Version.Message)
			}
			_ = json.NewEncoder(w).Encode(page)
		}))
		defer server.Close()

		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL, Token: "t"})
		handler := handleUpdateContent(client)
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{
					"contentId":      "123",
					"version":        float64(10),
					"versionComment": "update msg",
					"content":        "new content",
				},
			},
		}
		result, err := handler(ctx, req)
		if err != nil || result.IsError {
			t.Fatalf("handler failed: %v, %v", err, result)
		}
	})

	t.Run("update without new title/content (use current)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				_, _ = w.Write([]byte(`{"id":"123","type":"page","title":"Current Title","space":{"key":"TS"},"version":{"number":1},"body":{"storage":{"value":"Current Content","representation":"storage"}}}`))
				return
			}
			var page ConfluencePage
			_ = json.NewDecoder(r.Body).Decode(&page)
			if page.Title != "Current Title" {
				t.Errorf("expected current title, got %s", page.Title)
			}
			_ = json.NewEncoder(w).Encode(page)
		}))
		defer server.Close()

		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL, Token: "t"})
		handler := handleUpdateContent(client)
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{
					"contentId": "123",
				},
			},
		}
		result, err := handler(ctx, req)
		if err != nil || result.IsError {
			t.Fatalf("handler failed: %v, %v", err, result)
		}
	})

	t.Run("missing current version error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(ConfluencePage{ID: "123"}) // No version
		}))
		defer server.Close()

		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL, Token: "t"})
		handler := handleUpdateContent(client)
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{"contentId": "123"},
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError || !strings.Contains(result.Content[0].(mcp.TextContent).Text, "could not determine current version") {
			t.Error("expected version error")
		}
	})

	t.Run("invalid contentId format", func(t *testing.T) {
		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
		handler := handleUpdateContent(client)
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{"contentId": "../bad"},
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for bad contentId")
		}
	})

	t.Run("getArguments failure", func(t *testing.T) {
		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
		handler := handleUpdateContent(client)
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: "invalid",
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for invalid arguments")
		}
	})
}

// TestHandleListSpacesMore covers additional paths in handleListSpaces.
func TestHandleListSpacesMore(t *testing.T) {
	ctx := context.Background()

	t.Run("search with quotes", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cql := r.URL.Query().Get("cql")
			if !strings.Contains(cql, `\"quoted\"`) {
				t.Errorf("expected escaped quotes in CQL, got %s", cql)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()

		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL, Token: "t"})
		handler := handleListSpaces(client)
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{"searchText": `a "quoted" word`},
			},
		}
		result, err := handler(ctx, req)
		if err != nil || result.IsError {
			t.Fatalf("handler failed: %v, %v", err, result)
		}
	})

	t.Run("getArguments failure", func(t *testing.T) {
		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
		handler := handleListSpaces(client)
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: "invalid",
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for invalid arguments")
		}
	})
}

// TestHandleGetContentMore covers additional paths in handleGetContent.
func TestHandleGetContentMore(t *testing.T) {
	ctx := context.Background()
	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
	handler := handleGetContent(client)

	t.Run("getArguments failure", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: "invalid",
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for invalid arguments")
		}
	})
}

// TestHandleSearchContentMore covers additional paths in handleSearchContent.
func TestHandleSearchContentMore(t *testing.T) {
	ctx := context.Background()
	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
	handler := handleSearchContent(client)

	t.Run("getArguments failure", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: "invalid",
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for invalid arguments")
		}
	})
}

// TestHandleCreateContentMissingArgs covers missing required arguments in handleCreateContent.
func TestHandleCreateContentMissingArgs(t *testing.T) {
	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
	handler := handleCreateContent(client)
	ctx := context.Background()

	t.Run("missing spaceKey", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{"title": "T", "content": "C"},
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError || !strings.Contains(result.Content[0].(mcp.TextContent).Text, "spaceKey is required") {
			t.Error("expected spaceKey error")
		}
	})

	t.Run("missing content", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]any{"title": "T", "spaceKey": "S"},
			},
		}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError || !strings.Contains(result.Content[0].(mcp.TextContent).Text, "content is required") {
			t.Error("expected content error")
		}
	})
}

// TestTransportErrors covers transport level errors in ConfluenceClient.
func TestTransportErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("executeRequest connection failure", func(t *testing.T) {
		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://example.test", Token: "t"})
		failure := errors.New("connection failure")
		client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, failure
		})
		_, err := client.executeRequest(ctx, "GET", "/", nil, nil)
		if !errors.Is(err, failure) {
			t.Errorf("expected connection failure, got %v", err)
		}
	})

	t.Run("doRequest creation failure", func(t *testing.T) {
		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
		// Control characters in method or path can cause NewRequest to fail
		_, err := client.executeRequest(ctx, "IDK\x7f", "/", nil, nil)
		if err == nil {
			t.Error("expected error for invalid method")
		}
	})
}

// TestHandlerAPIErrors covers where handlers receive errors from the client.
func TestHandlerAPIErrors(t *testing.T) {
	ctx := context.Background()
	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://example.test", Token: "t"})
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection failure")
	})

	t.Run("handleSearchContent error", func(t *testing.T) {
		handler := handleSearchContent(client)
		req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"cql": "cql"}}}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for connection close")
		}
	})

	t.Run("handleCreateContent error", func(t *testing.T) {
		handler := handleCreateContent(client)
		req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"title": "T", "spaceKey": "S", "content": "C"}}}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for connection close")
		}
	})

	t.Run("handleUpdateContent error", func(t *testing.T) {
		handler := handleUpdateContent(client)
		req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"contentId": "123"}}}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for connection close")
		}
	})

	t.Run("handleListSpaces error", func(t *testing.T) {
		handler := handleListSpaces(client)
		req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{}}}
		result, err := handler(ctx, req)
		assertToolError(t, result, err)
		if !result.IsError {
			t.Error("expected error for connection close")
		}
	})
}

// TestHandleUpdateContentPutError tests the case where GET succeeds but PUT fails.
func TestHandleUpdateContentPutError(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"123","title":"Old","type":"page","space":{"key":"TS"},"body":{"storage":{"value":"<p>Old</p>","representation":"storage"}},"version":{"number":1}}`))
			return
		}
		// PUT fails
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("put failed"))
	}))
	defer server.Close()

	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL, Token: "t"})
	handler := handleUpdateContent(client)
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{"contentId": "123"},
		},
	}
	result, err := handler(ctx, req)
	assertToolError(t, result, err)
	if !result.IsError || !strings.Contains(result.Content[0].(mcp.TextContent).Text, "error updating content") {
		t.Errorf("expected update error, got %v", result.Content)
	}
}

// TestSetupServer tests the setupServer function.
func TestSetupServer(t *testing.T) {
	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://localhost", Token: "t"})
	s := setupServer(client)
	if s == nil {
		t.Fatal("setupServer returned nil")
	}
	expected := map[string][]string{
		"confluence_get_content":    {"contentId", "expand"},
		"confluence_search_content": {"cql", "limit", "start", "expand"},
		"confluence_create_content": {"title", "spaceKey", "content", "type", "parentId"},
		"confluence_update_content": {"contentId", "version", "title", "content", "versionComment"},
		"confluence_list_spaces":    {"searchText", "limit", "start", "expand"},
	}
	required := map[string][]string{
		"confluence_get_content": {"contentId"}, "confluence_search_content": {"cql"},
		"confluence_create_content": {"title", "spaceKey", "content"}, "confluence_update_content": {"contentId"},
	}
	tools := s.ListTools()
	if len(tools) != len(expected) {
		t.Fatalf("unexpected tool count %d", len(tools))
	}
	for name, args := range expected {
		tool := tools[name]
		if tool == nil {
			t.Fatalf("missing tool %s", name)
		}
		if !strings.Contains(tool.Tool.Description, "7.9.0 Server") || strings.Contains(tool.Tool.Description, "Data Center") {
			t.Errorf("wrong target description: %s", tool.Tool.Description)
		}
		properties := tool.Tool.InputSchema.Properties
		if len(properties) != len(args) {
			t.Errorf("%s properties: %v", name, properties)
		}
		for _, arg := range args {
			if _, ok := properties[arg]; !ok {
				t.Errorf("%s missing argument %s", name, arg)
			}
		}
		if !slices.Equal(tool.Tool.InputSchema.Required, required[name]) {
			t.Errorf("%s required: %v", name, tool.Tool.InputSchema.Required)
		}
		for _, arg := range []string{"limit", "start", "version"} {
			if property, ok := properties[arg]; ok {
				min := float64(0)
				if arg == "version" {
					min = 1
				}
				p := property.(map[string]any)
				if p["type"] != "number" || p["minimum"] != min || p["maximum"] != float64(maxRESTInt) || p["multipleOf"] != float64(1) {
					t.Errorf("%s.%s numeric constraints: %v", name, arg, p)
				}
			}
		}
	}
	typeProperty := tools["confluence_create_content"].Tool.InputSchema.Properties["type"].(map[string]any)
	if !reflect.DeepEqual(typeProperty["enum"], []string{"", "page", "blogpost"}) {
		t.Errorf("create type enum: %v", typeProperty)
	}
	response := s.HandleMessage(context.Background(), json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	rpc, ok := response.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("initialize response: %T %v", response, response)
	}
	initialized, ok := rpc.Result.(mcp.InitializeResult)
	if !ok || initialized.ServerInfo.Name != "confluence-7.9.0-go-mcp" || initialized.ServerInfo.Version != "1.0.0" {
		t.Fatalf("initialize result: %v", rpc.Result)
	}
}

// TestDoRequestReadError tests io.Read error in doRequest.
func TestDoRequestReadError(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		// Don't write enough bytes, then close
		_, _ = w.Write([]byte("too short"))
	}))
	defer server.Close()

	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL, Token: "t"})
	_, err := client.doRequest(ctx, "GET", "/", nil, nil)
	if err == nil {
		t.Error("expected error for truncated body")
	}
}

// TestRun tests the run function.
func TestRun(t *testing.T) {
	resetConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/api/user/current" || r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("bootstrap request: %s %s", r.Method, r.URL)
			http.Error(w, "invalid request", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"type":"known","username":"test-user"}`))
	}))
	defer server.Close()
	t.Run("success", func(t *testing.T) {
		t.Setenv("CONFLUENCE_API_TOKEN", "token")
		t.Setenv("CONFLUENCE_BASE_URL", server.URL)
		served := false
		err := run(func(s *mcpserver.MCPServer) error {
			served = true
			return nil // dummy serve
		})
		if err != nil || !served {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("config error", func(t *testing.T) {
		t.Setenv("CONFLUENCE_API_TOKEN", "") // trigger error
		err := run(func(s *mcpserver.MCPServer) error {
			return nil
		})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "configuration error") {
			t.Errorf("expected config error, got %v", err)
		}
	})

	t.Run("serve error", func(t *testing.T) {
		t.Setenv("CONFLUENCE_API_TOKEN", "token")
		t.Setenv("CONFLUENCE_BASE_URL", server.URL)
		err := run(func(s *mcpserver.MCPServer) error {
			return fmt.Errorf("serve failed")
		})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "server error") {
			t.Errorf("expected serve error, got %v", err)
		}
	})
}

func resetConfigEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, name := range []string{"CONFLUENCE_API_TOKEN", "CONFLUENCE_BASE_URL", "CONFLUENCE_API_BASE_PATH", "CONFLUENCE_HOST"} {
		// Register restoration before unsetting: empty ENV must override .env.
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func assertToolError(t *testing.T, result *mcp.CallToolResult, err error) {
	t.Helper()
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("expected tool-error + nil, got %v, %v", result, err)
	}
}

func assertToolText(t *testing.T, result *mcp.CallToolResult, err error, want string) {
	t.Helper()
	if err != nil || result == nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("expected successful text result, got %v, %v", result, err)
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok || text.Text != want {
		t.Fatalf("response changed: %v, want %q", result.Content, want)
	}
}

func TestLoadConfigServerContract(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"host", "example.test", "https://example.test/rest/api"},
		{"context", "http://example.test:8090/confluence/", "http://example.test:8090/confluence/rest/api"},
		{"REST root", "https://example.test/confluence/rest/api///", "https://example.test/confluence/rest/api"},
		{"suffix not substring", "https://example.test/rest/api-context", "https://example.test/rest/api-context/rest/api"},
		{"escaped context", "https://example.test/my%20wiki/", "https://example.test/my%20wiki/rest/api"},
		{"wrong scheme", "httpx://example.test", ""},
		{"missing host", "https:///confluence", ""},
		{"empty hostname", "http://:8090", ""},
		{"query", "https://example.test?status=draft", ""},
		{"empty query", "https://example.test?", ""},
		{"fragment", "https://example.test#fragment", ""},
		{"empty fragment", "https://example.test#", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetConfigEnv(t)
			t.Setenv("CONFLUENCE_API_TOKEN", "test-token")
			t.Setenv("CONFLUENCE_BASE_URL", tc.input)
			config, err := loadConfig()
			if tc.want == "" {
				if err == nil {
					t.Fatalf("accepted invalid URL: %s", tc.input)
				}
			} else if err != nil || config.BaseURL != tc.want || config.Token != "test-token" {
				t.Fatalf("config %v, %v; want %s", config, err, tc.want)
			}
		})
	}
	t.Run("precedence", func(t *testing.T) {
		resetConfigEnv(t)
		t.Setenv("CONFLUENCE_API_TOKEN", "test-token")
		t.Setenv("CONFLUENCE_BASE_URL", "https://base.test/wiki")
		t.Setenv("CONFLUENCE_API_BASE_PATH", "https://api.test/wiki")
		t.Setenv("CONFLUENCE_HOST", "host.test")
		for _, tc := range []struct{ clear, want string }{
			{"", "https://base.test/wiki/rest/api"},
			{"CONFLUENCE_BASE_URL", "https://api.test/wiki/rest/api"},
			{"CONFLUENCE_API_BASE_PATH", "https://host.test/rest/api"},
		} {
			if tc.clear != "" {
				t.Setenv(tc.clear, "")
			}
			config, err := loadConfig()
			if err != nil || config.BaseURL != tc.want {
				t.Fatalf("precedence: %v, %v; want %s", config, err, tc.want)
			}
		}
	})
}

func TestReadOptionalIntArgument(t *testing.T) {
	for _, name := range []string{"limit", "start", "version"} {
		min := 0
		if name == "version" {
			min = 1
		}
		for _, tc := range []struct {
			label string
			value any
			valid bool
		}{
			{"minimum", float64(min), true}, {"maximum", float64(maxRESTInt), true},
			{"below minimum", float64(min - 1), false}, {"overflow", float64(maxRESTInt) + 1, false},
			{"fraction", 1.5, false}, {"NaN", math.NaN(), false},
			{"positive infinity", math.Inf(1), false}, {"negative infinity", math.Inf(-1), false},
			{"string", "1", false}, {"integer Go type", 1, false}, {"null", nil, false},
		} {
			t.Run(name+"/"+tc.label, func(t *testing.T) {
				value, present, err := readOptionalIntArgument(map[string]any{name: tc.value}, name, min)
				if !present || (err == nil) != tc.valid {
					t.Fatalf("value=%d present=%v err=%v", value, present, err)
				}
				if tc.valid && float64(value) != tc.value.(float64) {
					t.Fatalf("changed number: %d, want %v", value, tc.value)
				}
			})
		}
		if _, present, err := readOptionalIntArgument(nil, name, min); present || err != nil {
			t.Fatalf("missing %s: present=%v err=%v", name, present, err)
		}
	}
	query, err := newPaginatedQuery(nil)
	if err != nil || !reflect.DeepEqual(query, url.Values{"limit": {"25"}}) {
		t.Fatalf("default query: %v, %v", query, err)
	}
}

func TestHandleValidationBeforeHTTP(t *testing.T) {
	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://example.test/rest/api", Token: "test"})
	calls := 0
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected HTTP call")
	})
	for _, name := range []string{"confluence_search_content", "confluence_list_spaces"} {
		for _, key := range []string{"limit", "start"} {
			for _, value := range []any{-1.0, 1.5, float64(maxRESTInt) + 1, "1", nil} {
				args := map[string]any{"cql": "type=page", key: value}
				tool := setupServer(client).GetTool(name)
				result, err := tool.Handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
				assertToolError(t, result, err)
			}
		}
	}
	for _, value := range []any{0.0, 1.5, float64(maxRESTInt) + 1, "1", nil} {
		result, err := handleUpdateContent(client)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"contentId": "123", "version": value}}})
		assertToolError(t, result, err)
	}
	for _, value := range []any{"attachment", 1.0, nil} {
		result, err := handleCreateContent(client)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"title": "T", "spaceKey": "TS", "content": "<p>C</p>", "type": value}}})
		assertToolError(t, result, err)
	}
	for _, name := range []string{"confluence_get_content", "confluence_update_content"} {
		for _, id := range []string{"123/456", "..123"} {
			result, err := setupServer(client).GetTool(name).Handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"contentId": id}}})
			assertToolError(t, result, err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid arguments issued %d HTTP calls", calls)
	}
}

func TestHandleReadQueries(t *testing.T) {
	content := `{"id":"123","type":"page","body":{"storage":{"value":"<p>C</p>","representation":"storage"}}}`
	search := `{"results":[{"content":{"id":"123","type":"page","space":{"key":"TS"},"body":{"storage":{"value":"<p>C</p>","representation":"storage"}}}}],"start":0,"limit":25,"_links":{"next":"/rest/api/search?start=25"}}`
	spaces := `{"results":[{"space":{"key":"TS","name":"Test","homepage":{"id":"123"}}}],"start":0,"limit":25,"_links":{"base":"http://example.test"}}`
	for _, tc := range []struct {
		name, tool, path, response string
		args                       map[string]any
		query                      url.Values
	}{
		{"get expansion", "confluence_get_content", "/content/123", content, map[string]any{"contentId": "123", "expand": "version", "limit": 10.0, "start": 2.0}, url.Values{"expand": {"version,body.storage"}}},
		{"get no duplicate", "confluence_get_content", "/content/123", content, map[string]any{"contentId": "123", "expand": "body.storage"}, url.Values{"expand": {"body.storage"}}},
		{"search", "confluence_search_content", "/search", search, map[string]any{"cql": `title ~ "a\\b"`, "expand": "content.body.storage,content.space"}, url.Values{"cql": {`title ~ "a\\b"`}, "limit": {"25"}, "expand": {"content.body.storage,content.space"}}},
		{"spaces default", "confluence_list_spaces", "/search", spaces, map[string]any{}, url.Values{"cql": {"type=space"}, "limit": {"25"}}},
		{"space title", "confluence_list_spaces", "/search", spaces, map[string]any{"searchText": `a\"quoted"*`, "limit": 0.0, "start": 2.0, "expand": "space.homepage"}, url.Values{"cql": {`type=space AND title ~ "a\\\"quoted\"*"`}, "limit": {"0"}, "start": {"2"}, "expand": {"space.homepage"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/confluence/rest/api"+tc.path || !reflect.DeepEqual(r.URL.Query(), tc.query) {
					t.Errorf("request %s %s; want GET %s %v", r.Method, r.URL, tc.path, tc.query)
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			client := NewConfluenceClient(&ConfluenceConfig{BaseURL: server.URL + "/confluence/rest/api", Token: "test"})
			result, err := setupServer(client).GetTool(tc.tool).Handler(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tc.args}})
			assertToolText(t, result, err, tc.response)
		})
	}
}

func TestHandleCreateContentPayload(t *testing.T) {
	for _, tc := range []struct {
		name, wantType, parent string
		typeArg                any
		setType                bool
	}{
		{"default page", "page", "", nil, false},
		{"empty default", "page", "", "", true},
		{"child page", "page", "123", "page", true},
		{"standalone blogpost", "blogpost", "", "blogpost", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := map[string]any{"type": tc.wantType, "title": "New", "space": map[string]any{"key": "TS"}, "body": map[string]any{"storage": map[string]any{"value": "<p>New</p>", "representation": "storage"}}}
			args := map[string]any{"title": "New", "spaceKey": "TS", "content": "<p>New</p>"}
			if tc.setType {
				args["type"] = tc.typeArg
			}
			if tc.parent != "" {
				args["parentId"] = tc.parent
				want["ancestors"] = []any{map[string]any{"id": tc.parent}}
			}
			client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://example.test/rest/api", Token: "test"})
			calls := 0
			client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/rest/api/content" || r.URL.RawQuery != "" {
					t.Errorf("unexpected create request: %s %s", r.Method, r.URL)
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("create payload %v, %v; want %v", got, err, want)
				}
				return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"id":"456"}`))}, nil
			})
			result, err := handleCreateContent(client)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
			assertToolText(t, result, err, `{"id":"456"}`)
			if calls != 1 {
				t.Fatalf("create calls: %d", calls)
			}
		})
	}
}

func TestHandleUpdateContentPreservation(t *testing.T) {
	const currentJSON = `{"id":"123","type":"page","title":"Old","space":{"key":"TS"},"body":{"storage":{"value":"<p>Old</p>","representation":"storage"}},"version":{"number":1}}`
	for _, tc := range []struct {
		name                   string
		args                   map[string]any
		alter                  func(map[string]any)
		wantTitle, wantBody    string
		wantVersion, putStatus int
		bodyless, wantError    bool
	}{
		{name: "preserve omitted"},
		{name: "preserve empty args", args: map[string]any{"title": "", "content": ""}},
		{name: "replace title", args: map[string]any{"title": "New"}, wantTitle: "New"},
		{name: "replace body", args: map[string]any{"content": "<p>New</p>"}, wantBody: "<p>New</p>", alter: func(m map[string]any) { delete(m, "body") }},
		{name: "replace absent fields", args: map[string]any{"title": "New", "content": "<p>New</p>"}, wantTitle: "New", wantBody: "<p>New</p>", alter: func(m map[string]any) { delete(m, "title"); delete(m, "body") }},
		{name: "explicit version without current", args: map[string]any{"version": 10.0, "versionComment": "note"}, wantVersion: 10, alter: func(m map[string]any) { delete(m, "version") }},
		{name: "empty stored body", alter: func(m map[string]any) {
			m["body"] = map[string]any{"storage": map[string]any{"value": "", "representation": "storage"}}
		}},
		{name: "blogpost", alter: func(m map[string]any) { m["type"] = "blogpost" }},
		{name: "bodyless attachment", bodyless: true, alter: func(m map[string]any) { m["type"] = "attachment"; delete(m, "body") }},
		{name: "missing type", wantError: true, alter: func(m map[string]any) { delete(m, "type") }},
		{name: "missing space", wantError: true, alter: func(m map[string]any) { delete(m, "space") }},
		{name: "empty space key", wantError: true, alter: func(m map[string]any) { m["space"] = map[string]any{"key": ""} }},
		{name: "missing title", wantError: true, alter: func(m map[string]any) { delete(m, "title") }},
		{name: "missing storage", wantError: true, alter: func(m map[string]any) { m["body"] = map[string]any{} }},
		{name: "missing storage value", wantError: true, alter: func(m map[string]any) {
			m["body"] = map[string]any{"storage": map[string]any{"representation": "storage"}}
		}},
		{name: "null storage value", wantError: true, alter: func(m map[string]any) {
			m["body"] = map[string]any{"storage": map[string]any{"value": nil, "representation": "storage"}}
		}},
		{name: "incomplete representation", wantError: true, alter: func(m map[string]any) { m["body"] = map[string]any{"storage": map[string]any{"value": "<p>Old</p>"}} }},
		{name: "missing version", wantError: true, alter: func(m map[string]any) { delete(m, "version") }},
		{name: "invalid current version", wantError: true, alter: func(m map[string]any) { m["version"] = map[string]any{"number": 0} }},
		{name: "increment overflow", wantError: true, alter: func(m map[string]any) { m["version"] = map[string]any{"number": maxRESTInt} }},
		{name: "conflict", putStatus: http.StatusConflict, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var current map[string]any
			if err := json.Unmarshal([]byte(currentJSON), &current); err != nil {
				t.Fatal(err)
			}
			if tc.alter != nil {
				tc.alter(current)
			}
			response, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"contentId": "123"}
			for k, v := range tc.args {
				args[k] = v
			}
			wantTitle, wantBody, wantVersion := "Old", "<p>Old</p>", 2
			if tc.wantTitle != "" {
				wantTitle = tc.wantTitle
			}
			if tc.wantBody != "" {
				wantBody = tc.wantBody
			}
			if tc.name == "empty stored body" {
				wantBody = ""
			}
			if tc.wantVersion != 0 {
				wantVersion = tc.wantVersion
			}
			version := map[string]any{"number": float64(wantVersion)}
			if tc.name == "explicit version without current" {
				version["message"] = "note"
			}
			want := map[string]any{"id": "123", "type": current["type"], "title": wantTitle, "space": map[string]any{"key": "TS"}, "version": version}
			if !tc.bodyless {
				want["body"] = map[string]any{"storage": map[string]any{"value": wantBody, "representation": "storage"}}
			}
			client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://example.test/rest/api", Token: "test"})
			var methods []string // Controlled transport runs synchronously in the test goroutine.
			client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				methods = append(methods, r.Method)
				if r.URL.Path != "/rest/api/content/123" {
					t.Errorf("unexpected path: %s", r.URL)
				}
				if r.Method == http.MethodGet {
					if !reflect.DeepEqual(r.URL.Query(), url.Values{"expand": {"body.storage,version,space"}}) {
						t.Errorf("unexpected update GET query: %s", r.URL)
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(response)))}, nil
				}
				if r.Method != http.MethodPut || r.URL.RawQuery != "" {
					t.Errorf("unexpected write request: %s %s", r.Method, r.URL)
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("update payload: %v, %v; want %v", got, err, want)
				}
				status := http.StatusOK
				if tc.putStatus != 0 {
					status = tc.putStatus
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"id":"123"}`))}, nil
			})
			result, err := handleUpdateContent(client)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
			if tc.wantError {
				assertToolError(t, result, err)
			} else {
				assertToolText(t, result, err, `{"id":"123"}`)
			}
			wantMethods := []string{http.MethodGet, http.MethodPut}
			if tc.wantError && tc.putStatus == 0 {
				wantMethods = []string{http.MethodGet}
			}
			if !slices.Equal(methods, wantMethods) {
				t.Fatalf("request sequence %v, want %v", methods, wantMethods)
			}
		})
	}
}

type trackingBody struct {
	io.Reader
	closed bool
}

func TestHandleUpdateContentGetFailure(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		status        int
	}{
		{"API failure", `{"message":"not found"}`, http.StatusNotFound},
		{"invalid JSON", `{invalid}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://example.test/rest/api", Token: "test"})
			calls := 0
			client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet {
					t.Errorf("must not PUT after failed GET: %s", r.Method)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.payload))}, nil
			})
			result, err := handleUpdateContent(client)(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"contentId": "123"}}})
			assertToolError(t, result, err)
			if calls != 1 {
				t.Fatalf("GET failure issued %d requests", calls)
			}
		})
	}
}

func (b *trackingBody) Close() error { b.closed = true; return nil }

func TestExecuteRequestContract(t *testing.T) {
	client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "https://example.test/confluence/rest/api", Token: "test-token"})
	if client.httpClient.Timeout != 30*time.Second {
		t.Fatalf("timeout: %v", client.httpClient.Timeout)
	}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "marker")
	body := &trackingBody{Reader: strings.NewReader(`{}`)}
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.String() != "https://example.test/confluence/rest/api/content?expand=space" || r.Context().Value(contextKey{}) != "marker" {
			t.Errorf("request/context changed: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("headers: %v", r.Header)
		}
		data, err := io.ReadAll(r.Body)
		if err != nil || string(data) != `{"type":"page"}` {
			t.Errorf("body: %s, %v", data, err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
	})
	response, err := client.executeRequest(ctx, http.MethodPost, "/content", url.Values{"expand": {"space"}}, map[string]string{"type": "page"})
	if err != nil {
		t.Fatal(err)
	}
	if body.closed {
		t.Fatal("executeRequest must transfer body ownership to its caller")
	}
	_ = response.Body.Close()
	if !body.closed {
		t.Fatal("caller did not close body")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})
	if _, err := client.executeRequest(ctx, http.MethodGet, "/content/123", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestResponseBodyClosure(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		status        int
		decode, fail  bool
	}{
		{"raw success", `{}`, http.StatusOK, false, false},
		{"raw API error", `{}`, http.StatusForbidden, false, true},
		{"JSON success", `{}`, http.StatusOK, true, false},
		{"JSON API error", `{}`, http.StatusForbidden, true, true},
		{"JSON decode error", `{invalid}`, http.StatusOK, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackingBody{Reader: strings.NewReader(tc.payload)}
			client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "http://example.test", Token: "test"})
			client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			})
			var err error
			if tc.decode {
				var target map[string]any
				err = client.getJSON(context.Background(), "/", nil, &target)
			} else {
				_, err = client.doRequest(context.Background(), http.MethodGet, "/", nil, nil)
			}
			if (err != nil) != tc.fail || !body.closed {
				t.Fatalf("err=%v closed=%v", err, body.closed)
			}
		})
	}
}

func TestLoadConfigDotenv(t *testing.T) {
	const file = "# Private configuration\nexport CONFLUENCE_API_TOKEN='file-token#=$LITERAL'\nCONFLUENCE_BASE_URL=\"https://file.test/wiki\"\n"
	for _, tc := range []struct {
		name, file, wantURL, wantToken, wantError string
		present                                   bool
		env                                       map[string]string
	}{
		{"file only", file, "https://file.test/wiki/rest/api", "file-token#=$LITERAL", "", true, nil},
		{"ENV only", "", "https://env.test/rest/api", "env-token", "", false, map[string]string{"CONFLUENCE_API_TOKEN": "env-token", "CONFLUENCE_BASE_URL": "env.test"}},
		{"missing file and settings", "", "", "", "CONFLUENCE_API_TOKEN", false, nil},
		{"empty file", "", "https://env.test/rest/api", "env-token", "", true, map[string]string{"CONFLUENCE_API_TOKEN": "env-token", "CONFLUENCE_HOST": "env.test"}},
		{"ENV wins same keys", file, "http://env.test/context/rest/api", "env-token", "", true, map[string]string{"CONFLUENCE_API_TOKEN": "env-token", "CONFLUENCE_BASE_URL": "http://env.test/context/"}},
		{"mixed sources", file, "https://file.test/wiki/rest/api", "env-token", "", true, map[string]string{"CONFLUENCE_API_TOKEN": "env-token"}},
		{"empty ENV token wins", file, "", "", "CONFLUENCE_API_TOKEN", true, map[string]string{"CONFLUENCE_API_TOKEN": ""}},
		{"alias precedence after merge", file, "https://file.test/wiki/rest/api", "file-token#=$LITERAL", "", true, map[string]string{"CONFLUENCE_HOST": "env.test"}},
		{"API alias", "CONFLUENCE_API_TOKEN='file-token'\nCONFLUENCE_API_BASE_PATH=https://api.test/wiki/rest/api/\nCONFLUENCE_HOST=host.test\n", "https://api.test/wiki/rest/api", "file-token", "", true, nil},
		{"empty ENV URL wins then alias", file + "CONFLUENCE_HOST=host.test\n", "https://host.test/rest/api", "file-token#=$LITERAL", "", true, map[string]string{"CONFLUENCE_BASE_URL": ""}},
		{"invalid dotenv", "CONFLUENCE_API_TOKEN='SECRET_DOTENV_MARKER\n", "", "", "cannot load .env", true, map[string]string{"CONFLUENCE_API_TOKEN": "env-token", "CONFLUENCE_HOST": "env.test"}},
		{"invalid URL without credentials in error", "CONFLUENCE_API_TOKEN='file-token'\nCONFLUENCE_BASE_URL='https://user:SECRET_DOTENV_MARKER@host.test/%zz'\n", "", "", "invalid base URL", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetConfigEnv(t)
			if tc.present {
				if err := os.WriteFile(".env", []byte(tc.file), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			config, err := loadConfig()
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) || strings.Contains(err.Error(), "SECRET_DOTENV_MARKER") {
					t.Fatalf("configuration error: %v", err)
				}
				return
			}
			if err != nil || config.BaseURL != tc.wantURL || config.Token != tc.wantToken {
				t.Fatalf("wrong configuration or unexpected error: %v", err)
			}
			if _, supplied := tc.env["CONFLUENCE_API_TOKEN"]; !supplied {
				if _, exists := os.LookupEnv("CONFLUENCE_API_TOKEN"); exists {
					t.Fatal("dotenv mutated process ENV")
				}
			}
		})
	}
	t.Run("unreadable file", func(t *testing.T) {
		resetConfigEnv(t)
		if err := os.Mkdir(".env", 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "cannot load .env") {
			t.Fatalf("accepted unreadable dotenv: %v", err)
		}
	})
	t.Run("no alternate lookup or unrelated ENV mutation", func(t *testing.T) {
		resetConfigEnv(t)
		t.Setenv("UNRELATED_DOTENV_KEY", "original")
		if err := os.WriteFile(".env", []byte(file+"UNRELATED_DOTENV_KEY=changed\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(); err != nil || os.Getenv("UNRELATED_DOTENV_KEY") != "original" {
			t.Fatalf("dotenv changed unrelated ENV: %v", err)
		}
		if err := os.WriteFile(".env.local", []byte(file), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir("child", 0700); err != nil {
			t.Fatal(err)
		}
		t.Chdir("child")
		if err := os.WriteFile(".env.local", []byte(file), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "CONFLUENCE_API_TOKEN") {
			t.Fatalf("loaded parent or .env.local: %v", err)
		}
	})
}

func TestCheckConnection(t *testing.T) {
	for _, tc := range []struct {
		name, payload, wantError string
		status                   int
	}{
		{"authenticated", `{"type":"known","username":"test-user"}`, "", 200},
		{"anonymous", `{"type":"anonymous"}`, "not authenticated", 200},
		{"missing type", `{}`, "not authenticated", 200},
		{"invalid type", `{"type":null}`, "not authenticated", 200},
		{"HTML login", `<html>SECRET_RESPONSE_MARKER</html>`, "JSON", 200},
		{"truncated JSON", `{"type":"known"`, "JSON", 200},
		{"trailing JSON", `{"type":"known"}{}`, "JSON", 200},
		{"bad request", `SECRET_RESPONSE_MARKER`, "HTTP 400 Bad Request", 400},
		{"unauthorized", `SECRET_RESPONSE_MARKER`, "HTTP 401 Unauthorized", 401},
		{"forbidden", `SECRET_RESPONSE_MARKER`, "HTTP 403 Forbidden", 403},
		{"not found", `SECRET_RESPONSE_MARKER`, "HTTP 404 Not Found", 404},
		{"unavailable", `SECRET_RESPONSE_MARKER`, "HTTP 503 Service Unavailable", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "https://example.test/confluence/rest/api", Token: "test-token"})
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, "marker")
			body := &trackingBody{Reader: strings.NewReader(tc.payload)}
			calls := 0
			client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.String() != "https://example.test/confluence/rest/api/user/current" || r.Context().Value(contextKey{}) != "marker" || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("bootstrap request/context/auth mismatch")
				}
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			})
			err := client.checkConnection(ctx)
			if tc.wantError == "" && err != nil || tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("bootstrap error: %v, want %q", err, tc.wantError)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET_RESPONSE_MARKER") {
				t.Fatal("response body leaked into bootstrap error")
			}
			if calls != 1 || !body.closed {
				t.Fatalf("calls=%d body.closed=%v", calls, body.closed)
			}
		})
	}
	t.Run("response read error", func(t *testing.T) {
		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "https://example.test/rest/api", Token: "test"})
		body := &trackingBody{Reader: iotest.ErrReader(errors.New("SECRET_RESPONSE_MARKER"))}
		client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: body}, nil
		})
		if err := client.checkConnection(context.Background()); err == nil || strings.Contains(err.Error(), "SECRET_RESPONSE_MARKER") || !body.closed {
			t.Fatalf("read error/closure: %v, closed=%v", err, body.closed)
		}
	})
	t.Run("cancellation and transport cause without URL userinfo", func(t *testing.T) {
		client := NewConfluenceClient(&ConfluenceConfig{BaseURL: "https://SECRET_URL_MARKER@example.test/rest/api", Token: "test"})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, r.Context().Err()
		})
		if err := client.checkConnection(ctx); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "SECRET_URL_MARKER") {
			t.Fatalf("transport cause/redaction: %v", err)
		}
	})
}

func TestRunDotenvBootstrap(t *testing.T) {
	for _, tc := range []struct {
		name, payload, wantError string
		status                   int
	}{
		{"success", `{"type":"known"}`, "", 200},
		{"unauthorized", `SECRET_RESPONSE_MARKER`, "HTTP 401", 401},
		{"anonymous", `{"type":"anonymous"}`, "not authenticated", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetConfigEnv(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/wiki/rest/api/user/current" || r.Header.Get("Authorization") != "Bearer dotenv-test-token" {
					t.Errorf("wrong bootstrap path/auth")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.payload))
			}))
			defer server.Close()
			file := fmt.Sprintf("CONFLUENCE_API_TOKEN='dotenv-test-token'\nCONFLUENCE_BASE_URL=%s/wiki\n", server.URL)
			if err := os.WriteFile(".env", []byte(file), 0600); err != nil {
				t.Fatal(err)
			}
			served := false
			err := run(func(*mcpserver.MCPServer) error { served = true; return nil })
			if tc.wantError == "" {
				if err != nil || !served {
					t.Fatalf("successful bootstrap did not serve: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "bootstrap error") || !strings.Contains(err.Error(), tc.wantError) || served || strings.Contains(err.Error(), "SECRET_RESPONSE_MARKER") {
				t.Fatalf("failed bootstrap err=%v served=%v", err, served)
			}
		})
	}
	t.Run("invalid dotenv never serves", func(t *testing.T) {
		resetConfigEnv(t)
		if err := os.WriteFile(".env", []byte("CONFLUENCE_API_TOKEN='SECRET_DOTENV_MARKER"), 0600); err != nil {
			t.Fatal(err)
		}
		served := false
		err := run(func(*mcpserver.MCPServer) error { served = true; return nil })
		if err == nil || !strings.Contains(err.Error(), "configuration error") || served || strings.Contains(err.Error(), "SECRET_DOTENV_MARKER") {
			t.Fatalf("configuration error err=%v served=%v", err, served)
		}
	})
}

// TestBinaryBootstrap also accepts an installed binary for post-install verification.
func TestBinaryBootstrap(t *testing.T) {
	binary := os.Getenv("CONFLUENCE_MCP_TEST_BINARY")
	if binary == "" {
		binary = filepath.Join(t.TempDir(), "confluence-mcp")
		build := exec.Command("go", "build", "-o", binary, ".")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build smoke binary: %v\n%s", err, output)
		}
	}
	for _, tc := range []struct {
		name, body, wantError string
		status                int
		invalidFile, offline  bool
	}{
		{"authenticated MCP", `{"type":"known","username":"test-user"}`, "", 200, false, false},
		{"HTTP 401", "SECRET_RESPONSE_MARKER", "HTTP 401 Unauthorized", 401, false, false},
		{"anonymous", `{"type":"anonymous"}`, "not authenticated", 200, false, false},
		{"invalid file", "", "configuration error: cannot load .env", 200, true, false},
		{"offline", "", "bootstrap error: request failed", 200, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/wiki/rest/api/user/current" || r.Header.Get("Authorization") != "Bearer binary-test-token" {
					t.Errorf("installed binary bootstrap path/auth mismatch")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer backend.Close()
			if tc.offline {
				backend.Close()
			}
			cwd := t.TempDir()
			file := fmt.Sprintf("CONFLUENCE_API_TOKEN='binary-test-token'\nCONFLUENCE_BASE_URL=%s/wiki\n", backend.URL)
			if tc.invalidFile {
				file = "CONFLUENCE_API_TOKEN='SECRET_DOTENV_MARKER"
			}
			if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte(file), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary)
			command.Dir = cwd
			for _, env := range os.Environ() {
				key, _, _ := strings.Cut(env, "=")
				if !slices.Contains([]string{"CONFLUENCE_API_TOKEN", "CONFLUENCE_BASE_URL", "CONFLUENCE_API_BASE_PATH", "CONFLUENCE_HOST"}, key) {
					command.Env = append(command.Env, env)
				}
			}
			var stderr bytes.Buffer
			command.Stderr = &stderr
			if tc.wantError != "" {
				var stdout bytes.Buffer
				command.Stdout = &stdout
				err := command.Run()
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), tc.wantError) {
					t.Fatalf("startup failure: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
				}
				if strings.Contains(stderr.String(), "SECRET_") || strings.Contains(stderr.String(), "binary-test-token") {
					t.Fatal("startup leaked credentials or response body")
				}
			} else {
				stdin, err := command.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				stdout, err := command.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := command.Start(); err != nil {
					t.Fatal(err)
				}
				_, writeErr := io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"binary-smoke","version":"1"}}}`+"\n")
				line, readErr := bufio.NewReader(stdout).ReadBytes('\n')
				_ = stdin.Close()
				waitErr := command.Wait()
				var reply struct {
					ID     int             `json:"id"`
					Error  json.RawMessage `json:"error"`
					Result struct {
						ServerInfo struct {
							Name string `json:"name"`
						} `json:"serverInfo"`
					} `json:"result"`
				}
				if writeErr != nil || readErr != nil || waitErr != nil || json.Unmarshal(line, &reply) != nil || reply.ID != 1 || len(reply.Error) != 0 || reply.Result.ServerInfo.Name != "confluence-7.9.0-go-mcp" || stderr.Len() != 0 {
					t.Fatalf("MCP initialization: write=%v read=%v exit=%v frame=%q stderr=%q", writeErr, readErr, waitErr, line, stderr.String())
				}
			}
			wantCalls := int32(1)
			if tc.invalidFile || tc.offline {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatalf("bootstrap made %d HTTP calls, want %d", calls.Load(), wantCalls)
			}
		})
	}
}

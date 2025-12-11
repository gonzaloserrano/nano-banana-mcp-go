package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		client:    nil, // nil client for unit tests that don't call Gemini
		outputDir: defaultOutputDir,
	}
}

func newTestServerWithOutputDir(t *testing.T, outputDir string) *Server {
	t.Helper()
	return &Server{
		client:    nil,
		outputDir: outputDir,
	}
}

func TestHandleInitialize(t *testing.T) {
	s := newTestServer(t)
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	}

	resp := s.handleRequest(req)

	require.NotNil(t, resp)
	require.Equal(t, "2.0", resp.JSONRPC)
	require.Equal(t, 1, resp.ID)
	require.Nil(t, resp.Error)

	result, ok := resp.Result.(InitializeResult)
	require.True(t, ok)
	require.Equal(t, protocolVersion, result.ProtocolVersion)
	require.Equal(t, serverName, result.ServerInfo.Name)
	require.Equal(t, serverVersion, result.ServerInfo.Version)
	require.NotNil(t, result.Capabilities.Tools)
}

func TestHandleListTools(t *testing.T) {
	s := newTestServer(t)
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	}

	resp := s.handleRequest(req)

	require.NotNil(t, resp)
	require.Nil(t, resp.Error)

	result, ok := resp.Result.(ListToolsResult)
	require.True(t, ok)
	require.Len(t, result.Tools, 2)

	toolNames := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		toolNames[i] = tool.Name
	}
	require.Contains(t, toolNames, "generate_image")
	require.Contains(t, toolNames, "edit_image")
}

func TestHandleCallToolUnknown(t *testing.T) {
	s := newTestServer(t)
	params, _ := json.Marshal(CallToolParams{
		Name:      "unknown_tool",
		Arguments: map[string]any{},
	})
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/call",
		Params:  params,
	}

	resp := s.handleRequest(req)

	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32601, resp.Error.Code)
	require.Contains(t, resp.Error.Message, "Unknown tool")
}

func TestHandleUnknownMethod(t *testing.T) {
	s := newTestServer(t)
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "unknown/method",
	}

	resp := s.handleRequest(req)

	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32601, resp.Error.Code)
	require.Equal(t, "Method not found", resp.Error.Message)
}

func TestHandleNotification(t *testing.T) {
	s := newTestServer(t)
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      nil,
		Method:  "notifications/initialized",
	}

	resp := s.handleRequest(req)

	require.Nil(t, resp, "notifications should not return a response")
}

func TestGenerateImageMissingPrompt(t *testing.T) {
	s := newTestServer(t)

	testCases := []struct {
		name string
		args map[string]any
	}{
		{
			name: "nil args",
			args: nil,
		},
		{
			name: "empty args",
			args: map[string]any{},
		},
		{
			name: "empty prompt",
			args: map[string]any{"prompt": ""},
		},
		{
			name: "wrong type",
			args: map[string]any{"prompt": 123},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.generateImage(tc.args)
			require.Error(t, err)
			require.Contains(t, err.Error(), "prompt is required")
		})
	}
}

func TestEditImageMissingParams(t *testing.T) {
	s := newTestServer(t)

	testCases := []struct {
		name        string
		args        map[string]any
		expectedErr string
	}{
		{
			name:        "missing image_path",
			args:        map[string]any{"prompt": "test"},
			expectedErr: "image_path is required",
		},
		{
			name:        "empty image_path",
			args:        map[string]any{"image_path": "", "prompt": "test"},
			expectedErr: "image_path is required",
		},
		{
			name:        "missing prompt",
			args:        map[string]any{"image_path": "/path/to/image.png"},
			expectedErr: "prompt is required",
		},
		{
			name:        "empty prompt",
			args:        map[string]any{"image_path": "/path/to/image.png", "prompt": ""},
			expectedErr: "prompt is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.editImage(tc.args)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.expectedErr)
		})
	}
}

func TestEditImagePathTraversal(t *testing.T) {
	s := newTestServer(t)

	// These paths contain ".." after filepath.Clean and should be rejected
	testCases := []struct {
		name string
		path string
	}{
		{
			name: "relative parent",
			path: "../etc/passwd",
		},
		{
			name: "multiple traversals",
			path: "../../secret.txt",
		},
		{
			name: "traversal in middle",
			path: "foo/../../../bar",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.editImage(map[string]any{
				"image_path": tc.path,
				"prompt":     "test prompt",
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "directory traversal not allowed")
		})
	}
}

func TestEditImageFileNotFound(t *testing.T) {
	s := newTestServer(t)

	_, err := s.editImage(map[string]any{
		"image_path": "/nonexistent/image.png",
		"prompt":     "test prompt",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read image")
}

func TestGetMimeType(t *testing.T) {
	testCases := []struct {
		path     string
		expected string
	}{
		{"image.png", "image/png"},
		{"image.PNG", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"photo.JPEG", "image/jpeg"},
		{"anim.gif", "image/gif"},
		{"modern.webp", "image/webp"},
		{"unknown.bmp", "image/png"},
		{"noext", "image/png"},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			result := getMimeType(tc.path)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestSaveImage(t *testing.T) {
	s := newTestServer(t)

	// Create temp directory for test
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)

	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
	})

	// Valid base64 PNG (1x1 red pixel)
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg=="

	path, err := s.saveImage(pngBase64, "test")
	require.NoError(t, err)
	require.NotEmpty(t, path)
	require.Contains(t, path, "test-")
	require.Contains(t, path, ".png")

	// Verify file exists and has content
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Greater(t, info.Size(), int64(0))
}

func TestSaveImageInvalidBase64(t *testing.T) {
	s := newTestServer(t)

	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)

	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
	})

	_, err = s.saveImage("not-valid-base64!!!", "test")
	require.Error(t, err)
}

func TestHandleCallToolInvalidParams(t *testing.T) {
	s := newTestServer(t)
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"invalid json`),
	}

	resp := s.handleRequest(req)

	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32602, resp.Error.Code)
}

func TestOutputDirCreation(t *testing.T) {
	s := newTestServer(t)

	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)

	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
	})

	// Verify output dir doesn't exist
	outputPath := filepath.Join(tmpDir, defaultOutputDir)
	_, err = os.Stat(outputPath)
	require.True(t, os.IsNotExist(err))

	// Save image should create the directory
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg=="
	_, err = s.saveImage(pngBase64, "test")
	require.NoError(t, err)

	// Verify directory was created
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestCustomOutputDir(t *testing.T) {
	customDir := "my-custom-images"
	s := newTestServerWithOutputDir(t, customDir)

	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)

	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
	})

	// Save image should create the custom directory
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg=="
	path, err := s.saveImage(pngBase64, "test")
	require.NoError(t, err)
	require.Contains(t, path, customDir)

	// Verify custom directory was created
	outputPath := filepath.Join(tmpDir, customDir)
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

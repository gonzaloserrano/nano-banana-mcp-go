// Package main implements a Model Context Protocol (MCP) server for AI image
// generation and editing using Google Gemini API.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/genai"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "nano-banana-mcp"
	serverVersion   = "2.0.0"

	geminiModel      = "gemini-2.5-flash-image"
	defaultOutputDir = "generated"
)

// MCP JSON-RPC types
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP types
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

type Capabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct{}

type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema JSONSchema `json:"inputSchema"`
}

type JSONSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Server
type Server struct {
	client    *genai.Client
	outputDir string
}

func NewServer(client *genai.Client, outputDir string) *Server {
	return &Server{
		client:    client,
		outputDir: outputDir,
	}
}

func (s *Server) handleRequest(req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return nil // No response for notifications
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleCallTool(req)
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: -32601, Message: "Method not found"},
		}
	}
}

func (s *Server) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: InitializeResult{
			ProtocolVersion: protocolVersion,
			Capabilities: Capabilities{
				Tools: &ToolsCapability{},
			},
			ServerInfo: ServerInfo{
				Name:    serverName,
				Version: serverVersion,
			},
		},
	}
}

func (s *Server) handleListTools(req *JSONRPCRequest) *JSONRPCResponse {
	tools := []Tool{
		{
			Name:        "generate_image",
			Description: "Generate a new image from a text prompt using Google Gemini",
			InputSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"prompt": {
						Type:        "string",
						Description: "Text description of the image to generate",
					},
				},
				Required: []string{"prompt"},
			},
		},
		{
			Name:        "edit_image",
			Description: "Edit an existing image using a text prompt",
			InputSchema: JSONSchema{
				Type: "object",
				Properties: map[string]Property{
					"image_path": {
						Type:        "string",
						Description: "Path to the image file to edit",
					},
					"prompt": {
						Type:        "string",
						Description: "Text description of the edits to make",
					},
				},
				Required: []string{"image_path", "prompt"},
			},
		},
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  ListToolsResult{Tools: tools},
	}
}

func (s *Server) handleCallTool(req *JSONRPCRequest) *JSONRPCResponse {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	var result CallToolResult
	var err error

	switch params.Name {
	case "generate_image":
		result, err = s.generateImage(params.Arguments)
	case "edit_image":
		result, err = s.editImage(params.Arguments)
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: -32601, Message: "Unknown tool: " + params.Name},
		}
	}

	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: CallToolResult{
				Content: []Content{{Type: "text", Text: "Error: " + err.Error()}},
				IsError: true,
			},
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) generateImage(args map[string]any) (CallToolResult, error) {
	prompt, ok := args["prompt"].(string)
	if !ok || prompt == "" {
		return CallToolResult{}, fmt.Errorf("prompt is required")
	}

	ctx := context.Background()
	result, err := s.client.Models.GenerateContent(ctx, geminiModel, genai.Text(prompt), nil)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("API request failed: %w", err)
	}

	var imageData, textResponse string
	for _, part := range result.Candidates[0].Content.Parts {
		if part.Text != "" {
			textResponse += part.Text
		} else if part.InlineData != nil {
			imageData = base64.StdEncoding.EncodeToString(part.InlineData.Data)
		}
	}

	if imageData == "" {
		msg := "No image was generated."
		if textResponse != "" {
			msg += "\n\nModel response: " + textResponse
		}
		return CallToolResult{
			Content: []Content{{Type: "text", Text: msg}},
		}, nil
	}

	filePath, err := s.saveImage(imageData, "generated")
	if err != nil {
		return CallToolResult{}, fmt.Errorf("failed to save image: %w", err)
	}

	content := []Content{
		{Type: "text", Text: fmt.Sprintf("Image generated and saved to: %s\n\nPrompt: %s", filePath, prompt)},
		{Type: "image", Data: imageData, MimeType: "image/png"},
	}

	return CallToolResult{Content: content}, nil
}

func (s *Server) editImage(args map[string]any) (CallToolResult, error) {
	imagePath, ok := args["image_path"].(string)
	if !ok || imagePath == "" {
		return CallToolResult{}, fmt.Errorf("image_path is required")
	}

	prompt, ok := args["prompt"].(string)
	if !ok || prompt == "" {
		return CallToolResult{}, fmt.Errorf("prompt is required")
	}

	cleanPath := filepath.Clean(imagePath)
	if strings.Contains(cleanPath, "..") {
		return CallToolResult{}, fmt.Errorf("invalid image path: directory traversal not allowed")
	}

	imageBytes, err := os.ReadFile(cleanPath)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("failed to read image: %w", err)
	}

	ctx := context.Background()
	result, err := s.client.Models.GenerateContent(ctx, geminiModel,
		[]*genai.Content{{
			Parts: []*genai.Part{
				{InlineData: &genai.Blob{MIMEType: getMimeType(cleanPath), Data: imageBytes}},
				{Text: prompt},
			},
		}},
		nil,
	)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("API request failed: %w", err)
	}

	var imageData, textResponse string
	for _, part := range result.Candidates[0].Content.Parts {
		if part.Text != "" {
			textResponse += part.Text
		} else if part.InlineData != nil {
			imageData = base64.StdEncoding.EncodeToString(part.InlineData.Data)
		}
	}

	if imageData == "" {
		msg := "No edited image was generated."
		if textResponse != "" {
			msg += "\n\nModel response: " + textResponse
		}
		return CallToolResult{
			Content: []Content{{Type: "text", Text: msg}},
		}, nil
	}

	filePath, err := s.saveImage(imageData, "edited")
	if err != nil {
		return CallToolResult{}, fmt.Errorf("failed to save image: %w", err)
	}

	content := []Content{
		{Type: "text", Text: fmt.Sprintf("Image edited and saved to: %s\n\nOriginal: %s\nPrompt: %s", filePath, imagePath, prompt)},
		{Type: "image", Data: imageData, MimeType: "image/png"},
	}

	return CallToolResult{Content: content}, nil
}

func (s *Server) saveImage(base64Data, prefix string) (string, error) {
	dir := filepath.Join(".", s.outputDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	timestamp := time.Now().Format("2006-01-02T15-04-05")
	filename := fmt.Sprintf("%s-%s.png", prefix, timestamp)
	filePath := filepath.Join(dir, filename)

	imageBytes, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(filePath, imageBytes, 0644); err != nil {
		return "", err
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}
	return absPath, nil
}

func getMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

func (s *Server) Run() error {
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		resp := s.handleRequest(&req)
		if resp == nil {
			continue
		}

		respJSON, err := json.Marshal(resp)
		if err != nil {
			continue
		}

		fmt.Println(string(respJSON))
		_ = os.Stdout.Sync()
	}

	return scanner.Err()
}

func main() {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Gemini client: %v\n", err)
		os.Exit(1)
	}

	outputDir := defaultOutputDir
	if len(os.Args) > 1 {
		outputDir = os.Args[1]
	}

	server := NewServer(client, outputDir)
	if err := server.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

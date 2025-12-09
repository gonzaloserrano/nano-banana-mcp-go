package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// Gemini API types
type GeminiRequest struct {
	Contents         []GeminiContent   `json:"contents"`
	GenerationConfig *GenerationConfig `json:"generationConfig,omitempty"`
}

type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *InlineData `json:"inlineData,omitempty"`
}

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type GenerationConfig struct {
	ResponseModalities []string `json:"responseModalities,omitempty"`
}

type GeminiResponse struct {
	Candidates []Candidate `json:"candidates"`
}

type Candidate struct {
	Content GeminiContent `json:"content"`
}

// Server
type Server struct {
	apiKey string
}

func NewServer() *Server {
	return &Server{
		apiKey: os.Getenv("GEMINI_API_KEY"),
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
			ProtocolVersion: "2024-11-05",
			Capabilities: Capabilities{
				Tools: &ToolsCapability{},
			},
			ServerInfo: ServerInfo{
				Name:    "nano-banana-mcp",
				Version: "2.0.0",
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
	if s.apiKey == "" {
		return CallToolResult{}, fmt.Errorf("GEMINI_API_KEY environment variable not set")
	}

	prompt, ok := args["prompt"].(string)
	if !ok || prompt == "" {
		return CallToolResult{}, fmt.Errorf("prompt is required")
	}

	// Call Gemini API
	imageData, textResponse, err := s.callGemini(prompt, nil)
	if err != nil {
		return CallToolResult{}, err
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

	// Save image
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
	if s.apiKey == "" {
		return CallToolResult{}, fmt.Errorf("GEMINI_API_KEY environment variable not set")
	}

	imagePath, ok := args["image_path"].(string)
	if !ok || imagePath == "" {
		return CallToolResult{}, fmt.Errorf("image_path is required")
	}

	prompt, ok := args["prompt"].(string)
	if !ok || prompt == "" {
		return CallToolResult{}, fmt.Errorf("prompt is required")
	}

	// Read source image
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("failed to read image: %w", err)
	}

	sourceImage := &InlineData{
		MimeType: getMimeType(imagePath),
		Data:     base64.StdEncoding.EncodeToString(imageBytes),
	}

	// Call Gemini API
	imageData, textResponse, err := s.callGemini(prompt, sourceImage)
	if err != nil {
		return CallToolResult{}, err
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

	// Save image
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

func (s *Server) callGemini(prompt string, sourceImage *InlineData) (imageData, textResponse string, err error) {
	parts := []GeminiPart{}

	if sourceImage != nil {
		parts = append(parts, GeminiPart{InlineData: sourceImage})
	}
	parts = append(parts, GeminiPart{Text: prompt})

	reqBody := GeminiRequest{
		Contents: []GeminiContent{{Parts: parts}},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash-exp:generateContent?key=%s", s.apiKey)
	resp, err := http.Post(url, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return "", "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return "", "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract image and text from response
	for _, candidate := range geminiResp.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				imageData = part.InlineData.Data
			}
			if part.Text != "" {
				textResponse += part.Text
			}
		}
	}

	return imageData, textResponse, nil
}

func (s *Server) saveImage(base64Data, prefix string) (string, error) {
	dir := filepath.Join(".", "generated_imgs")
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

	absPath, _ := filepath.Abs(filePath)
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
	reader := bufio.NewReader(os.Stdin)

	for {
		// Read Content-Length header
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Content-Length:") {
			continue
		}

		var contentLength int
		fmt.Sscanf(line, "Content-Length: %d", &contentLength)

		// Skip empty line
		reader.ReadString('\n')

		// Read JSON body
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, body); err != nil {
			return err
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
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

		fmt.Printf("Content-Length: %d\r\n\r\n%s", len(respJSON), respJSON)
	}
}

func main() {
	server := NewServer()
	if err := server.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

# Nano Banana MCP Go

An MCP server for AI image generation/editing using Google Gemini.

Go port of https://github.com/ConechoAI/Nano-Banana-MCP and https://github.com/TinDang97/Nano-Banana-MCP.

## Setup

1. Get a Gemini API key from [Google AI Studio](https://aistudio.google.com/apikey)

2. Configure your MCP client:

```json
{
  "mcpServers": {
    "nano-banana": {
      "command": "go",
      "args": ["run", "github.com/gonzaloserrano/nano-banana-mcp-go@latest"],
      "env": {
        "GEMINI_API_KEY": "your-api-key"
      }
    }
  }
}
```

Images are saved to `./generated/`. Pass a custom directory as the first argument if needed.

## Development

```bash
make check  # lint + tests
```

## License

MIT

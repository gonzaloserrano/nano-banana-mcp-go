# Nano Banana MCP

A simple MCP server for AI image generation using Google Gemini.

## Features

- **generate_image** - Generate images from text prompts
- **edit_image** - Edit existing images with text prompts

## Setup

1. Get a Gemini API key from [Google AI Studio](https://aistudio.google.com/apikey)

2. Build the server:
```bash
go build -o nano-banana-mcp .
```

3. Configure your MCP client (e.g., Claude Code):

```json
{
  "mcpServers": {
    "nano-banana": {
      "command": "/path/to/nano-banana-mcp",
      "env": {
        "GEMINI_API_KEY": "your-api-key"
      }
    }
  }
}
```

## Usage

### Generate an image

```
Generate an image of a sunset over mountains
```

### Edit an image

```
Edit /path/to/image.png: add a rainbow in the sky
```

Images are saved to `./generated_imgs/`.

## License

MIT

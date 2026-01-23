import DocLayout from '../components/DocLayout'
import CodeBlock from '../components/CodeBlock'

export default function ClientAPI() {
  return (
    <DocLayout title="Client API" description="Complete reference for the ZAP Go client.">
      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Client Type</h2>
        <p className="text-zinc-300 mb-4">
          The <code>Client</code> struct is the main interface for ZAP communication:
        </p>
        <CodeBlock
          language="go"
          code={`type Client struct {
    // Internal fields - not exported
}

// Thread-safe for concurrent use after initialization`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Connect</h2>
        <p className="text-zinc-300 mb-4">
          Creates a new client connection to a ZAP server:
        </p>
        <CodeBlock
          language="go"
          code={`func Connect(addr string) (*Client, error)`}
        />
        <CodeBlock
          language="go"
          code={`client, err := zap.Connect("localhost:9999")
if err != nil {
    return fmt.Errorf("connection failed: %w", err)
}
defer client.Close()`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Close</h2>
        <p className="text-zinc-300 mb-4">
          Closes the connection. Safe to call multiple times:
        </p>
        <CodeBlock
          language="go"
          code={`func (c *Client) Close() error`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Init</h2>
        <p className="text-zinc-300 mb-4">
          Initializes the connection with client metadata:
        </p>
        <CodeBlock
          language="go"
          code={`func (c *Client) Init(ctx context.Context, name, version string) (*ServerInfoData, error)`}
        />
        <CodeBlock
          language="go"
          code={`info, err := client.Init(ctx, "my-agent", "1.0.0")
if err != nil {
    return err
}

// Check server capabilities
if info.Capabilities.Tools {
    // Server supports tools
}
if info.Capabilities.Resources {
    // Server supports resources
}
if info.Capabilities.Prompts {
    // Server supports prompts
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">ListTools</h2>
        <p className="text-zinc-300 mb-4">
          Returns all available tools:
        </p>
        <CodeBlock
          language="go"
          code={`func (c *Client) ListTools(ctx context.Context) ([]ToolData, error)`}
        />
        <CodeBlock
          language="go"
          code={`tools, err := client.ListTools(ctx)
if err != nil {
    return err
}

for _, tool := range tools {
    fmt.Printf("Name: %s\\n", tool.Name)
    fmt.Printf("Description: %s\\n", tool.Description)
    fmt.Printf("Schema: %s\\n", tool.Schema)
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">CallTool</h2>
        <p className="text-zinc-300 mb-4">
          Invokes a tool with arguments:
        </p>
        <CodeBlock
          language="go"
          code={`func (c *Client) CallTool(ctx context.Context, name string, args any) (*ToolResultData, error)`}
        />
        <CodeBlock
          language="go"
          code={`// Arguments are JSON-serialized automatically
result, err := client.CallTool(ctx, "search", map[string]any{
    "query":  "golang concurrency",
    "limit":  10,
    "offset": 0,
})
if err != nil {
    return err
}

// Check for tool-level errors
if result.Error != "" {
    return fmt.Errorf("tool error: %s", result.Error)
}

// Parse result content
var data SearchResults
if err := json.Unmarshal(result.Content, &data); err != nil {
    return err
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">ListResources</h2>
        <p className="text-zinc-300 mb-4">
          Returns all available resources:
        </p>
        <CodeBlock
          language="go"
          code={`func (c *Client) ListResources(ctx context.Context) ([]ResourceData, error)`}
        />
        <CodeBlock
          language="go"
          code={`resources, err := client.ListResources(ctx)
if err != nil {
    return err
}

for _, res := range resources {
    fmt.Printf("URI: %s\\n", res.URI)
    fmt.Printf("Name: %s\\n", res.Name)
    fmt.Printf("MimeType: %s\\n", res.MimeType)
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">ReadResource</h2>
        <p className="text-zinc-300 mb-4">
          Reads content from a resource:
        </p>
        <CodeBlock
          language="go"
          code={`func (c *Client) ReadResource(ctx context.Context, uri string) (*ResourceContentData, error)`}
        />
        <CodeBlock
          language="go"
          code={`content, err := client.ReadResource(ctx, "file:///path/to/file.txt")
if err != nil {
    return err
}

if content.Text != "" {
    fmt.Println("Text content:", content.Text)
} else if len(content.Blob) > 0 {
    fmt.Printf("Binary content: %d bytes\\n", len(content.Blob))
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">ListPrompts</h2>
        <p className="text-zinc-300 mb-4">
          Returns all available prompts:
        </p>
        <CodeBlock
          language="go"
          code={`func (c *Client) ListPrompts(ctx context.Context) ([]PromptData, error)`}
        />
        <CodeBlock
          language="go"
          code={`prompts, err := client.ListPrompts(ctx)
if err != nil {
    return err
}

for _, p := range prompts {
    fmt.Printf("Prompt: %s - %s\\n", p.Name, p.Description)
    for _, arg := range p.Arguments {
        required := ""
        if arg.Required {
            required = " (required)"
        }
        fmt.Printf("  Arg: %s%s\\n", arg.Name, required)
    }
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">GetPrompt</h2>
        <p className="text-zinc-300 mb-4">
          Retrieves a prompt with arguments:
        </p>
        <CodeBlock
          language="go"
          code={`func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) ([]PromptMessageData, error)`}
        />
        <CodeBlock
          language="go"
          code={`messages, err := client.GetPrompt(ctx, "code-review", map[string]string{
    "language": "go",
    "focus":    "performance",
})
if err != nil {
    return err
}

for _, msg := range messages {
    fmt.Printf("[%s]: %s\\n", msg.Role, msg.Content.Text)
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Log</h2>
        <p className="text-zinc-300 mb-4">
          Sends a log message to the server:
        </p>
        <CodeBlock
          language="go"
          code={`func (c *Client) Log(ctx context.Context, level LogLevel, message string, data any) error

// Log levels
const (
    LogLevelDebug LogLevel = iota
    LogLevelInfo
    LogLevelWarn
    LogLevelError
)`}
        />
        <CodeBlock
          language="go"
          code={`// Simple log
err := client.Log(ctx, zap.LogLevelInfo, "Processing complete", nil)

// Log with structured data
err := client.Log(ctx, zap.LogLevelError, "Request failed", map[string]any{
    "request_id": "abc123",
    "duration":   1.5,
    "error":      "connection timeout",
})`}
        />
      </section>

      <section>
        <h2 className="text-2xl font-semibold mb-4">Error Handling</h2>
        <p className="text-zinc-300 mb-4">
          All methods return errors following Go conventions:
        </p>
        <CodeBlock
          language="go"
          code={`result, err := client.CallTool(ctx, "search", args)
if err != nil {
    // Network/protocol error
    return fmt.Errorf("RPC failed: %w", err)
}

if result.Error != "" {
    // Tool execution error
    return fmt.Errorf("tool failed: %s", result.Error)
}

// Success
process(result.Content)`}
        />
      </section>
    </DocLayout>
  )
}

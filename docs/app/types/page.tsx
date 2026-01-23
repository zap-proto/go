import DocLayout from '../components/DocLayout'
import CodeBlock from '../components/CodeBlock'

export default function Types() {
  return (
    <DocLayout title="Types" description="Data type reference for ZAP Go.">
      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">ToolData</h2>
        <p className="text-zinc-300 mb-4">
          Represents a tool definition returned by <code>ListTools()</code>:
        </p>
        <CodeBlock
          language="go"
          code={`type ToolData struct {
    // Name is the unique identifier for the tool
    Name string \`json:"name"\`

    // Description explains what the tool does
    Description string \`json:"description"\`

    // Schema is the JSON Schema for tool arguments
    Schema json.RawMessage \`json:"schema,omitempty"\`

    // Annotations are optional metadata key-value pairs
    Annotations map[string]string \`json:"annotations,omitempty"\`
}`}
        />
        <CodeBlock
          language="go"
          code={`// Example usage
tools, _ := client.ListTools(ctx)
for _, tool := range tools {
    fmt.Printf("Tool: %s\\n", tool.Name)
    fmt.Printf("  Description: %s\\n", tool.Description)

    // Parse schema if needed
    var schema map[string]any
    json.Unmarshal(tool.Schema, &schema)
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">ToolResultData</h2>
        <p className="text-zinc-300 mb-4">
          Result of a tool call from <code>CallTool()</code>:
        </p>
        <CodeBlock
          language="go"
          code={`type ToolResultData struct {
    // ID uniquely identifies this result
    ID string \`json:"id"\`

    // Content is the tool's output (JSON)
    Content json.RawMessage \`json:"content,omitempty"\`

    // Error message if the tool failed
    Error string \`json:"error,omitempty"\`

    // Metadata contains optional key-value pairs
    Metadata map[string]string \`json:"metadata,omitempty"\`
}`}
        />
        <CodeBlock
          language="go"
          code={`// Example usage
result, _ := client.CallTool(ctx, "search", args)

if result.Error != "" {
    log.Printf("Tool error: %s", result.Error)
    return
}

// Parse typed content
var searchResults struct {
    Items []struct {
        Title string \`json:"title"\`
        URL   string \`json:"url"\`
    } \`json:"items"\`
}
json.Unmarshal(result.Content, &searchResults)`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">ResourceData</h2>
        <p className="text-zinc-300 mb-4">
          Describes an available resource from <code>ListResources()</code>:
        </p>
        <CodeBlock
          language="go"
          code={`type ResourceData struct {
    // URI is the unique identifier for the resource
    URI string \`json:"uri"\`

    // Name is a human-readable name
    Name string \`json:"name"\`

    // Description explains what the resource contains
    Description string \`json:"description"\`

    // MimeType indicates the content type
    MimeType string \`json:"mimeType"\`

    // Annotations are optional metadata
    Annotations map[string]string \`json:"annotations,omitempty"\`
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">ResourceContentData</h2>
        <p className="text-zinc-300 mb-4">
          Content returned by <code>ReadResource()</code>:
        </p>
        <CodeBlock
          language="go"
          code={`type ResourceContentData struct {
    // URI identifies the resource
    URI string \`json:"uri"\`

    // MimeType of the content
    MimeType string \`json:"mimeType"\`

    // Text content (mutually exclusive with Blob)
    Text string \`json:"text,omitempty"\`

    // Blob content for binary data (mutually exclusive with Text)
    Blob []byte \`json:"blob,omitempty"\`
}`}
        />
        <CodeBlock
          language="go"
          code={`// Example: handling different content types
content, _ := client.ReadResource(ctx, uri)

switch {
case content.Text != "":
    // Text content
    fmt.Println(content.Text)

case len(content.Blob) > 0:
    // Binary content
    if strings.HasPrefix(content.MimeType, "image/") {
        // Handle image
        ioutil.WriteFile("output.png", content.Blob, 0644)
    }
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">PromptData</h2>
        <p className="text-zinc-300 mb-4">
          Describes an available prompt from <code>ListPrompts()</code>:
        </p>
        <CodeBlock
          language="go"
          code={`type PromptData struct {
    // Name is the unique identifier
    Name string \`json:"name"\`

    // Description explains the prompt's purpose
    Description string \`json:"description"\`

    // Arguments lists the prompt parameters
    Arguments []PromptArgument \`json:"arguments,omitempty"\`
}

type PromptArgument struct {
    // Name of the argument
    Name string \`json:"name"\`

    // Description of what this argument does
    Description string \`json:"description"\`

    // Required indicates if this argument must be provided
    Required bool \`json:"required"\`
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">PromptMessageData</h2>
        <p className="text-zinc-300 mb-4">
          A message returned by <code>GetPrompt()</code>:
        </p>
        <CodeBlock
          language="go"
          code={`type PromptMessageData struct {
    // Role is "user", "assistant", or "system"
    Role string \`json:"role"\`

    // Content of the message
    Content ContentData \`json:"content"\`
}

type ContentData struct {
    // Text content
    Text string \`json:"text,omitempty"\`

    // Image content
    Image *ImageContentData \`json:"image,omitempty"\`

    // Embedded resource content
    Resource *ResourceContentData \`json:"resource,omitempty"\`
}

type ImageContentData struct {
    // Data is the raw image bytes
    Data []byte \`json:"data"\`

    // MimeType (e.g., "image/png")
    MimeType string \`json:"mimeType"\`
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">ServerInfoData</h2>
        <p className="text-zinc-300 mb-4">
          Server information returned by <code>Init()</code>:
        </p>
        <CodeBlock
          language="go"
          code={`type ServerInfoData struct {
    // Name of the server
    Name string \`json:"name"\`

    // Version of the server
    Version string \`json:"version"\`

    // Capabilities supported by the server
    Capabilities CapabilitiesData \`json:"capabilities"\`
}

type CapabilitiesData struct {
    // Tools indicates tools/list and tools/call are supported
    Tools bool \`json:"tools"\`

    // Resources indicates resources/list and resources/read are supported
    Resources bool \`json:"resources"\`

    // Prompts indicates prompts/list and prompts/get are supported
    Prompts bool \`json:"prompts"\`

    // Logging indicates log messages are accepted
    Logging bool \`json:"logging"\`
}`}
        />
      </section>

      <section>
        <h2 className="text-2xl font-semibold mb-4">LogLevel</h2>
        <p className="text-zinc-300 mb-4">
          Log severity levels for <code>Log()</code>:
        </p>
        <CodeBlock
          language="go"
          code={`type LogLevel int

const (
    LogLevelDebug LogLevel = iota  // 0
    LogLevelInfo                    // 1
    LogLevelWarn                    // 2
    LogLevelError                   // 3
)`}
        />
        <CodeBlock
          language="go"
          code={`// Usage
client.Log(ctx, zap.LogLevelDebug, "Debug message", nil)
client.Log(ctx, zap.LogLevelInfo, "Info message", data)
client.Log(ctx, zap.LogLevelWarn, "Warning", nil)
client.Log(ctx, zap.LogLevelError, "Error occurred", errorData)`}
        />
      </section>
    </DocLayout>
  )
}

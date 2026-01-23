import DocLayout from '../components/DocLayout'
import CodeBlock from '../components/CodeBlock'

export default function QuickStart() {
  return (
    <DocLayout title="Quick Start" description="Connect to a ZAP server and call your first tool.">
      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Connect to a Server</h2>
        <p className="text-zinc-300 mb-4">
          The <code>Connect</code> function establishes a TCP connection to a ZAP server:
        </p>
        <CodeBlock
          language="go"
          filename="main.go"
          code={`package main

import (
    "context"
    "log"

    zap "github.com/zap-protocol/zap-go"
)

func main() {
    // Connect to ZAP server
    client, err := zap.Connect("localhost:9999")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    log.Println("Connected to ZAP server")
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Initialize Connection</h2>
        <p className="text-zinc-300 mb-4">
          Initialize the connection with client metadata and receive server capabilities:
        </p>
        <CodeBlock
          language="go"
          code={`ctx := context.Background()

// Initialize with client info
serverInfo, err := client.Init(ctx, "my-agent", "1.0.0")
if err != nil {
    log.Fatal(err)
}

log.Printf("Server: %s v%s", serverInfo.Name, serverInfo.Version)
log.Printf("Capabilities: tools=%v, resources=%v, prompts=%v",
    serverInfo.Capabilities.Tools,
    serverInfo.Capabilities.Resources,
    serverInfo.Capabilities.Prompts,
)`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">List Available Tools</h2>
        <p className="text-zinc-300 mb-4">
          Discover what tools the server provides:
        </p>
        <CodeBlock
          language="go"
          code={`tools, err := client.ListTools(ctx)
if err != nil {
    log.Fatal(err)
}

for _, tool := range tools {
    log.Printf("Tool: %s - %s", tool.Name, tool.Description)
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Call a Tool</h2>
        <p className="text-zinc-300 mb-4">
          Invoke a tool with arguments and handle the result:
        </p>
        <CodeBlock
          language="go"
          code={`result, err := client.CallTool(ctx, "search", map[string]any{
    "query": "hello world",
    "limit": 10,
})
if err != nil {
    log.Fatal(err)
}

if result.Error != "" {
    log.Printf("Tool error: %s", result.Error)
} else {
    log.Printf("Result: %s", result.Content)
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Complete Example</h2>
        <p className="text-zinc-300 mb-4">
          Here's a complete working example:
        </p>
        <CodeBlock
          language="go"
          filename="main.go"
          code={`package main

import (
    "context"
    "log"

    zap "github.com/zap-protocol/zap-go"
)

func main() {
    // Connect to ZAP server
    client, err := zap.Connect("localhost:9999")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()

    // Initialize connection
    serverInfo, err := client.Init(ctx, "my-agent", "1.0.0")
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Connected to %s v%s", serverInfo.Name, serverInfo.Version)

    // List available tools
    tools, err := client.ListTools(ctx)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Available tools: %d", len(tools))

    for _, tool := range tools {
        log.Printf("  - %s: %s", tool.Name, tool.Description)
    }

    // Call a tool if available
    if len(tools) > 0 {
        result, err := client.CallTool(ctx, tools[0].Name, map[string]any{})
        if err != nil {
            log.Fatal(err)
        }
        log.Printf("Result: %s", result.Content)
    }
}`}
        />
      </section>

      <section>
        <h2 className="text-2xl font-semibold mb-4">Next Steps</h2>
        <ul className="list-disc list-inside space-y-2 text-zinc-300">
          <li>
            <a href="/client/" className="text-primary-500 hover:underline">Client API</a> - Full API reference
          </li>
          <li>
            <a href="/types/" className="text-primary-500 hover:underline">Types</a> - Data type documentation
          </li>
          <li>
            <a href="/examples/" className="text-primary-500 hover:underline">Examples</a> - More code examples
          </li>
        </ul>
      </section>
    </DocLayout>
  )
}

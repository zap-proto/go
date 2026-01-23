import DocLayout from '../components/DocLayout'
import CodeBlock from '../components/CodeBlock'

export default function Examples() {
  return (
    <DocLayout title="Examples" description="Practical code examples for ZAP Go.">
      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Basic Client</h2>
        <p className="text-zinc-300 mb-4">
          Minimal example connecting to a server and listing tools:
        </p>
        <CodeBlock
          language="go"
          filename="basic/main.go"
          code={`package main

import (
    "context"
    "log"

    zap "github.com/zap-protocol/zap-go"
)

func main() {
    client, err := zap.Connect("localhost:9999")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()

    info, err := client.Init(ctx, "basic-client", "1.0.0")
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Connected to %s v%s", info.Name, info.Version)

    tools, err := client.ListTools(ctx)
    if err != nil {
        log.Fatal(err)
    }

    for _, tool := range tools {
        log.Printf("Tool: %s - %s", tool.Name, tool.Description)
    }
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Tool Execution with Retry</h2>
        <p className="text-zinc-300 mb-4">
          Robust tool execution with exponential backoff:
        </p>
        <CodeBlock
          language="go"
          filename="retry/main.go"
          code={`package main

import (
    "context"
    "fmt"
    "log"
    "time"

    zap "github.com/zap-protocol/zap-go"
)

func callWithRetry(
    client *zap.Client,
    ctx context.Context,
    name string,
    args any,
    maxRetries int,
) (*zap.ToolResultData, error) {
    var lastErr error

    for i := 0; i < maxRetries; i++ {
        result, err := client.CallTool(ctx, name, args)
        if err == nil && result.Error == "" {
            return result, nil
        }

        if err != nil {
            lastErr = err
        } else {
            lastErr = fmt.Errorf("tool error: %s", result.Error)
        }

        // Exponential backoff: 100ms, 200ms, 400ms, ...
        backoff := time.Duration(100<<i) * time.Millisecond
        log.Printf("Retry %d/%d after %v: %v", i+1, maxRetries, backoff, lastErr)
        time.Sleep(backoff)
    }

    return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func main() {
    client, err := zap.Connect("localhost:9999")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()
    client.Init(ctx, "retry-client", "1.0.0")

    result, err := callWithRetry(client, ctx, "flaky-tool", map[string]any{
        "input": "test",
    }, 3)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Success: %s", result.Content)
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Concurrent Tool Calls</h2>
        <p className="text-zinc-300 mb-4">
          Execute multiple tool calls in parallel:
        </p>
        <CodeBlock
          language="go"
          filename="concurrent/main.go"
          code={`package main

import (
    "context"
    "log"
    "sync"

    zap "github.com/zap-protocol/zap-go"
)

type ToolCall struct {
    Name string
    Args any
}

type ToolResult struct {
    Call   ToolCall
    Result *zap.ToolResultData
    Error  error
}

func callToolsConcurrently(
    client *zap.Client,
    ctx context.Context,
    calls []ToolCall,
) []ToolResult {
    results := make([]ToolResult, len(calls))
    var wg sync.WaitGroup

    for i, call := range calls {
        wg.Add(1)
        go func(idx int, c ToolCall) {
            defer wg.Done()
            result, err := client.CallTool(ctx, c.Name, c.Args)
            results[idx] = ToolResult{
                Call:   c,
                Result: result,
                Error:  err,
            }
        }(i, call)
    }

    wg.Wait()
    return results
}

func main() {
    client, err := zap.Connect("localhost:9999")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()
    client.Init(ctx, "concurrent-client", "1.0.0")

    calls := []ToolCall{
        {Name: "search", Args: map[string]any{"query": "golang"}},
        {Name: "search", Args: map[string]any{"query": "rust"}},
        {Name: "search", Args: map[string]any{"query": "python"}},
    }

    results := callToolsConcurrently(client, ctx, calls)

    for _, r := range results {
        if r.Error != nil {
            log.Printf("Error calling %s: %v", r.Call.Name, r.Error)
        } else {
            log.Printf("Result for %s: %s", r.Call.Name, r.Result.Content)
        }
    }
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Resource Reader</h2>
        <p className="text-zinc-300 mb-4">
          List and read resources from a server:
        </p>
        <CodeBlock
          language="go"
          filename="resources/main.go"
          code={`package main

import (
    "context"
    "log"
    "os"
    "path/filepath"
    "strings"

    zap "github.com/zap-protocol/zap-go"
)

func main() {
    client, err := zap.Connect("localhost:9999")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()

    info, err := client.Init(ctx, "resource-reader", "1.0.0")
    if err != nil {
        log.Fatal(err)
    }

    if !info.Capabilities.Resources {
        log.Fatal("Server does not support resources")
    }

    // List all resources
    resources, err := client.ListResources(ctx)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Found %d resources", len(resources))

    // Read each resource
    for _, res := range resources {
        log.Printf("Reading: %s (%s)", res.Name, res.URI)

        content, err := client.ReadResource(ctx, res.URI)
        if err != nil {
            log.Printf("  Error: %v", err)
            continue
        }

        if content.Text != "" {
            // Text content - print preview
            preview := content.Text
            if len(preview) > 100 {
                preview = preview[:100] + "..."
            }
            log.Printf("  Text: %s", preview)
        } else if len(content.Blob) > 0 {
            // Binary content - save to file
            filename := filepath.Base(strings.TrimPrefix(content.URI, "file://"))
            err := os.WriteFile(filename, content.Blob, 0644)
            if err != nil {
                log.Printf("  Error saving: %v", err)
            } else {
                log.Printf("  Saved %d bytes to %s", len(content.Blob), filename)
            }
        }
    }
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Prompt Templating</h2>
        <p className="text-zinc-300 mb-4">
          Retrieve and format prompts:
        </p>
        <CodeBlock
          language="go"
          filename="prompts/main.go"
          code={`package main

import (
    "context"
    "fmt"
    "log"
    "strings"

    zap "github.com/zap-protocol/zap-go"
)

func formatMessages(messages []zap.PromptMessageData) string {
    var sb strings.Builder

    for _, msg := range messages {
        sb.WriteString(fmt.Sprintf("[%s]\\n", strings.ToUpper(msg.Role)))

        if msg.Content.Text != "" {
            sb.WriteString(msg.Content.Text)
        } else if msg.Content.Image != nil {
            sb.WriteString(fmt.Sprintf("<image: %s, %d bytes>",
                msg.Content.Image.MimeType,
                len(msg.Content.Image.Data)))
        } else if msg.Content.Resource != nil {
            sb.WriteString(fmt.Sprintf("<resource: %s>",
                msg.Content.Resource.URI))
        }

        sb.WriteString("\\n\\n")
    }

    return sb.String()
}

func main() {
    client, err := zap.Connect("localhost:9999")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()
    client.Init(ctx, "prompt-client", "1.0.0")

    // List available prompts
    prompts, err := client.ListPrompts(ctx)
    if err != nil {
        log.Fatal(err)
    }

    for _, p := range prompts {
        log.Printf("Prompt: %s", p.Name)
        log.Printf("  Description: %s", p.Description)
        for _, arg := range p.Arguments {
            required := ""
            if arg.Required {
                required = " (required)"
            }
            log.Printf("  - %s: %s%s", arg.Name, arg.Description, required)
        }
    }

    // Get a specific prompt
    if len(prompts) > 0 {
        messages, err := client.GetPrompt(ctx, prompts[0].Name, map[string]string{
            "language": "go",
            "task":     "write unit tests",
        })
        if err != nil {
            log.Fatal(err)
        }

        fmt.Println("\\n--- Generated Prompt ---")
        fmt.Println(formatMessages(messages))
    }
}`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Structured Logging</h2>
        <p className="text-zinc-300 mb-4">
          Send structured logs to the server:
        </p>
        <CodeBlock
          language="go"
          filename="logging/main.go"
          code={`package main

import (
    "context"
    "log"
    "time"

    zap "github.com/zap-protocol/zap-go"
)

type RequestLog struct {
    RequestID string        \`json:"request_id"\`
    Method    string        \`json:"method"\`
    Duration  time.Duration \`json:"duration_ns"\`
    Status    string        \`json:"status"\`
}

func main() {
    client, err := zap.Connect("localhost:9999")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()

    info, err := client.Init(ctx, "logging-client", "1.0.0")
    if err != nil {
        log.Fatal(err)
    }

    if !info.Capabilities.Logging {
        log.Println("Warning: server does not support logging")
    }

    // Log at different levels
    client.Log(ctx, zap.LogLevelDebug, "Starting operation", nil)

    start := time.Now()

    // Simulate work
    result, err := client.CallTool(ctx, "process", map[string]any{
        "input": "data",
    })

    duration := time.Since(start)

    if err != nil {
        client.Log(ctx, zap.LogLevelError, "Operation failed", map[string]any{
            "error":    err.Error(),
            "duration": duration.String(),
        })
        log.Fatal(err)
    }

    // Log structured data
    client.Log(ctx, zap.LogLevelInfo, "Operation completed", RequestLog{
        RequestID: result.ID,
        Method:    "process",
        Duration:  duration,
        Status:    "success",
    })
}`}
        />
      </section>

      <section>
        <h2 className="text-2xl font-semibold mb-4">Context Cancellation</h2>
        <p className="text-zinc-300 mb-4">
          Handle timeouts and cancellation properly:
        </p>
        <CodeBlock
          language="go"
          filename="context/main.go"
          code={`package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    zap "github.com/zap-protocol/zap-go"
)

func main() {
    client, err := zap.Connect("localhost:9999")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Create cancellable context
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Handle graceful shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-sigChan
        log.Println("Shutting down...")
        cancel()
    }()

    client.Init(ctx, "context-client", "1.0.0")

    // Call with timeout
    timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
    defer timeoutCancel()

    result, err := client.CallTool(timeoutCtx, "slow-operation", map[string]any{
        "input": "data",
    })
    if err != nil {
        if ctx.Err() == context.Canceled {
            log.Println("Operation was cancelled")
        } else if timeoutCtx.Err() == context.DeadlineExceeded {
            log.Println("Operation timed out")
        } else {
            log.Printf("Operation failed: %v", err)
        }
        return
    }

    log.Printf("Result: %s", result.Content)
}`}
        />
      </section>
    </DocLayout>
  )
}

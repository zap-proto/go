# ZAP Go

> **Docs:** [ZAP Go SDK](https://zap-proto.dev/docs/sdks/go) · part of the [ZAP Protocol](https://zap-proto.io)


Go bindings for **ZAP** (Zero-Copy App Proto) - high-performance Cap'n Proto RPC for AI agents.

## Installation

```bash
go get github.com/zap-protocol/zap-go
```

## Usage

```go
package main

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

    // List available tools
    tools, err := client.ListTools(context.Background())
    if err != nil {
        log.Fatal(err)
    }

    for _, tool := range tools {
        log.Printf("Tool: %s - %s", tool.Name(), tool.Description())
    }

    // Call a tool
    result, err := client.CallTool(context.Background(), "search", map[string]any{
        "query": "hello world",
    })
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Result: %s", result.Content())
}
```

## Regenerating from Schema

```bash
# Install capnp compiler and Go plugin
go install capnproto.org/go/capnp/v3/capnpc-go@latest

# Generate Go code from schema
capnp compile -I$GOPATH/src/capnproto.org/go/capnp/std -ogo zap.capnp
```

## Features

- Zero-copy message passing via Cap'n Proto
- Full ZAP protocol support (tools, resources, prompts)
- MCP Gateway bridging
- Post-quantum cryptography (ML-KEM, ML-DSA, Ringtail)
- W3C DID support
- Agent consensus voting

## Related Packages

- [zap-protocol/zap](https://github.com/zap-protocol/zap) - Core schema + Rust implementation
- [zap-protocol/zap-ts](https://github.com/zap-protocol/zap-ts) - TypeScript bindings
- [zap-protocol/zap-py](https://github.com/zap-protocol/zap-py) - Python bindings
- [zap-protocol/zap-cpp](https://github.com/zap-protocol/zap-cpp) - C/C++ bindings

## License

MIT
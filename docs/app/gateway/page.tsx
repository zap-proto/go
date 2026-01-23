import DocLayout from '../components/DocLayout'
import CodeBlock from '../components/CodeBlock'

export default function Gateway() {
  return (
    <DocLayout title="Gateway" description="Bridge ZAP to MCP servers and other protocols.">
      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Overview</h2>
        <p className="text-zinc-300 mb-4">
          ZAP includes gateway capabilities to bridge between ZAP's high-performance Cap'n Proto
          protocol and other protocols like MCP (Model Context Protocol). This enables ZAP clients
          to access tools from existing MCP servers.
        </p>
        <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4 my-6">
          <pre className="text-sm text-zinc-300">
{`┌─────────────┐     Cap'n Proto      ┌─────────────┐      JSON-RPC       ┌─────────────┐
│  ZAP Client │ ──────────────────▶ │ ZAP Gateway │ ──────────────────▶ │  MCP Server │
│    (Go)     │                      │             │                      │             │
└─────────────┘                      └─────────────┘                      └─────────────┘`}
          </pre>
        </div>
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">MCP Bridge</h2>
        <p className="text-zinc-300 mb-4">
          Connect to an MCP server through a ZAP gateway:
        </p>
        <CodeBlock
          language="go"
          code={`// Connect to ZAP gateway that bridges to MCP
client, err := zap.Connect("gateway.example.com:9999")
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// Initialize - gateway will forward to MCP server
info, err := client.Init(ctx, "my-agent", "1.0.0")
if err != nil {
    log.Fatal(err)
}

// MCP tools are now available through ZAP
tools, err := client.ListTools(ctx)
if err != nil {
    log.Fatal(err)
}

// Call MCP tools via ZAP (zero-copy transport)
result, err := client.CallTool(ctx, "filesystem_read", map[string]any{
    "path": "/tmp/data.json",
})`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Protocol Translation</h2>
        <p className="text-zinc-300 mb-4">
          The gateway handles protocol translation automatically:
        </p>
        <div className="overflow-x-auto">
          <table className="w-full text-left border-collapse">
            <thead>
              <tr className="border-b border-zinc-700">
                <th className="py-3 px-4 text-zinc-300 font-semibold">ZAP Method</th>
                <th className="py-3 px-4 text-zinc-300 font-semibold">MCP Equivalent</th>
              </tr>
            </thead>
            <tbody className="text-zinc-400">
              <tr className="border-b border-zinc-800">
                <td className="py-3 px-4"><code>Init()</code></td>
                <td className="py-3 px-4"><code>initialize</code></td>
              </tr>
              <tr className="border-b border-zinc-800">
                <td className="py-3 px-4"><code>ListTools()</code></td>
                <td className="py-3 px-4"><code>tools/list</code></td>
              </tr>
              <tr className="border-b border-zinc-800">
                <td className="py-3 px-4"><code>CallTool()</code></td>
                <td className="py-3 px-4"><code>tools/call</code></td>
              </tr>
              <tr className="border-b border-zinc-800">
                <td className="py-3 px-4"><code>ListResources()</code></td>
                <td className="py-3 px-4"><code>resources/list</code></td>
              </tr>
              <tr className="border-b border-zinc-800">
                <td className="py-3 px-4"><code>ReadResource()</code></td>
                <td className="py-3 px-4"><code>resources/read</code></td>
              </tr>
              <tr className="border-b border-zinc-800">
                <td className="py-3 px-4"><code>ListPrompts()</code></td>
                <td className="py-3 px-4"><code>prompts/list</code></td>
              </tr>
              <tr className="border-b border-zinc-800">
                <td className="py-3 px-4"><code>GetPrompt()</code></td>
                <td className="py-3 px-4"><code>prompts/get</code></td>
              </tr>
              <tr className="border-b border-zinc-800">
                <td className="py-3 px-4"><code>Log()</code></td>
                <td className="py-3 px-4"><code>logging/setLevel</code></td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Performance Benefits</h2>
        <p className="text-zinc-300 mb-4">
          Using ZAP gateway provides significant performance improvements over direct MCP:
        </p>
        <ul className="list-disc list-inside space-y-2 text-zinc-300">
          <li><strong>Zero-copy</strong> - Cap'n Proto eliminates serialization overhead</li>
          <li><strong>Binary protocol</strong> - Smaller message sizes than JSON-RPC</li>
          <li><strong>Multiplexing</strong> - Multiple concurrent requests over single connection</li>
          <li><strong>Promise pipelining</strong> - Chain calls without round-trip latency</li>
        </ul>
        <CodeBlock
          language="go"
          code={`// Benchmark comparison (typical workload)
//
// Direct MCP (JSON-RPC over stdio):
//   tools/list:  ~2.5ms avg
//   tools/call:  ~5.0ms avg
//
// ZAP Gateway (Cap'n Proto over TCP):
//   ListTools:   ~0.3ms avg
//   CallTool:    ~0.8ms avg
//
// ~3-6x performance improvement`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Gateway Configuration</h2>
        <p className="text-zinc-300 mb-4">
          When running your own ZAP gateway, configure MCP backend servers:
        </p>
        <CodeBlock
          language="yaml"
          filename="gateway.yaml"
          code={`# ZAP Gateway configuration
listen: "0.0.0.0:9999"

backends:
  - name: "filesystem"
    type: "mcp"
    command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/data"]

  - name: "github"
    type: "mcp"
    command: ["npx", "-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "\${GITHUB_TOKEN}"

  - name: "postgres"
    type: "mcp"
    command: ["npx", "-y", "@modelcontextprotocol/server-postgres"]
    env:
      DATABASE_URL: "\${DATABASE_URL}"`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Multi-Server Aggregation</h2>
        <p className="text-zinc-300 mb-4">
          A gateway can aggregate tools from multiple MCP servers:
        </p>
        <CodeBlock
          language="go"
          code={`// Connect to gateway aggregating multiple MCP servers
client, err := zap.Connect("gateway.example.com:9999")
if err != nil {
    log.Fatal(err)
}

// Lists tools from ALL backend servers
tools, err := client.ListTools(ctx)
// Returns: filesystem_read, filesystem_write, github_create_issue,
//          postgres_query, etc.

// Gateway routes to correct backend automatically
result, err := client.CallTool(ctx, "github_create_issue", map[string]any{
    "repo":  "zap-protocol/zap-go",
    "title": "Bug report",
    "body":  "Description...",
})`}
        />
      </section>

      <section>
        <h2 className="text-2xl font-semibold mb-4">Security</h2>
        <p className="text-zinc-300 mb-4">
          ZAP supports post-quantum cryptography for secure gateway communication:
        </p>
        <ul className="list-disc list-inside space-y-2 text-zinc-300">
          <li><strong>ML-KEM</strong> - Key encapsulation (Kyber)</li>
          <li><strong>ML-DSA</strong> - Digital signatures (Dilithium)</li>
          <li><strong>W3C DID</strong> - Decentralized identity verification</li>
        </ul>
        <CodeBlock
          language="go"
          code={`// Post-quantum secure connection (when supported by server)
// Key exchange uses ML-KEM, signatures use ML-DSA
client, err := zap.ConnectSecure("gateway.example.com:9999", &zap.SecurityConfig{
    DID:        "did:key:z6Mk...",
    PrivateKey: privateKey,
})`}
        />
      </section>
    </DocLayout>
  )
}

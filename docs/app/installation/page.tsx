import DocLayout from '../components/DocLayout'
import CodeBlock from '../components/CodeBlock'

export default function Installation() {
  return (
    <DocLayout title="Installation" description="Get ZAP Go up and running in your project.">
      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Requirements</h2>
        <ul className="list-disc list-inside space-y-2 text-zinc-300">
          <li>Go 1.23 or later</li>
          <li>Cap'n Proto compiler (for schema regeneration)</li>
        </ul>
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Install Package</h2>
        <p className="text-zinc-300 mb-4">
          Add ZAP Go to your project using <code>go get</code>:
        </p>
        <CodeBlock
          language="bash"
          code={`go get github.com/zap-protocol/zap-go`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Import</h2>
        <p className="text-zinc-300 mb-4">
          Import the package in your Go code:
        </p>
        <CodeBlock
          language="go"
          code={`import zap "github.com/zap-protocol/zap-go"`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Cap'n Proto Setup (Optional)</h2>
        <p className="text-zinc-300 mb-4">
          If you need to regenerate the schema bindings, install the Cap'n Proto compiler:
        </p>
        <CodeBlock
          language="bash"
          code={`# macOS
brew install capnp

# Ubuntu/Debian
apt-get install capnproto

# Install Go plugin
go install capnproto.org/go/capnp/v3/capnpc-go@latest`}
        />
        <p className="text-zinc-300 mt-4 mb-4">
          Regenerate bindings from schema:
        </p>
        <CodeBlock
          language="bash"
          code={`capnp compile -I$GOPATH/src/capnproto.org/go/capnp/std -ogo zap.capnp`}
        />
      </section>

      <section className="mb-12">
        <h2 className="text-2xl font-semibold mb-4">Verify Installation</h2>
        <p className="text-zinc-300 mb-4">
          Create a simple test file to verify the installation:
        </p>
        <CodeBlock
          language="go"
          filename="main.go"
          code={`package main

import (
    "fmt"

    zap "github.com/zap-protocol/zap-go"
)

func main() {
    // Verify package is accessible
    fmt.Println("ZAP Go installed successfully!")
    fmt.Printf("LogLevel types: Debug=%d, Info=%d, Warn=%d, Error=%d\\n",
        zap.LogLevelDebug,
        zap.LogLevelInfo,
        zap.LogLevelWarn,
        zap.LogLevelError,
    )
}`}
        />
        <CodeBlock
          language="bash"
          code={`go run main.go
# Output: ZAP Go installed successfully!
# Output: LogLevel types: Debug=0, Info=1, Warn=2, Error=3`}
        />
      </section>

      <section>
        <h2 className="text-2xl font-semibold mb-4">Next Steps</h2>
        <p className="text-zinc-300">
          Now that ZAP Go is installed, head to the{' '}
          <a href="/quickstart/" className="text-primary-500 hover:underline">
            Quick Start
          </a>{' '}
          guide to connect to your first ZAP server.
        </p>
      </section>
    </DocLayout>
  )
}

import Link from 'next/link'
import { Zap, Package, BookOpen, Code, Server, FileCode } from 'lucide-react'

const features = [
  {
    icon: Zap,
    title: 'Zero-Copy Performance',
    description: 'Cap\'n Proto RPC delivers minimal memory overhead and maximum throughput.',
  },
  {
    icon: Server,
    title: 'MCP Gateway',
    description: 'Bridge to Model Context Protocol servers for seamless AI tool integration.',
  },
  {
    icon: Code,
    title: 'Type-Safe API',
    description: 'Fully typed Go bindings with ergonomic data structures.',
  },
]

const sections = [
  { href: '/installation/', title: 'Installation', icon: Package, description: 'Get started with ZAP Go' },
  { href: '/quickstart/', title: 'Quick Start', icon: Zap, description: 'Your first ZAP client' },
  { href: '/client/', title: 'Client API', icon: Code, description: 'Complete client reference' },
  { href: '/gateway/', title: 'Gateway', icon: Server, description: 'MCP Gateway bridging' },
  { href: '/types/', title: 'Types', icon: FileCode, description: 'Data type reference' },
  { href: '/examples/', title: 'Examples', icon: BookOpen, description: 'Code examples' },
]

export default function Home() {
  return (
    <div className="min-h-screen">
      {/* Header */}
      <header className="border-b border-zinc-800">
        <div className="max-w-6xl mx-auto px-6 py-4 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Zap className="w-8 h-8 text-primary-500" />
            <span className="text-xl font-semibold">ZAP Go</span>
          </div>
          <nav className="flex items-center gap-6">
            <Link href="/installation/" className="text-zinc-400 hover:text-white transition">
              Docs
            </Link>
            <a
              href="https://github.com/zap-protocol/zap-go"
              className="text-zinc-400 hover:text-white transition"
              target="_blank"
              rel="noopener noreferrer"
            >
              GitHub
            </a>
          </nav>
        </div>
      </header>

      {/* Hero */}
      <section className="py-24 px-6">
        <div className="max-w-4xl mx-auto text-center">
          <h1 className="text-5xl font-bold mb-6">
            <span className="text-primary-500">ZAP</span> Go
          </h1>
          <p className="text-xl text-zinc-400 mb-8 max-w-2xl mx-auto">
            High-performance Go bindings for ZAP (Zero-Copy App Proto) -
            Cap'n Proto RPC for AI agent communication.
          </p>
          <div className="flex items-center justify-center gap-4">
            <Link
              href="/quickstart/"
              className="px-6 py-3 bg-primary-500 text-black font-medium rounded-lg hover:bg-primary-400 transition"
            >
              Get Started
            </Link>
            <Link
              href="/client/"
              className="px-6 py-3 bg-zinc-800 text-white font-medium rounded-lg hover:bg-zinc-700 transition"
            >
              API Reference
            </Link>
          </div>
        </div>
      </section>

      {/* Install */}
      <section className="py-12 px-6 bg-zinc-900/50">
        <div className="max-w-4xl mx-auto">
          <pre className="text-center text-lg">
            <code>go get github.com/zap-protocol/zap-go</code>
          </pre>
        </div>
      </section>

      {/* Features */}
      <section className="py-24 px-6">
        <div className="max-w-6xl mx-auto">
          <h2 className="text-3xl font-bold text-center mb-12">Features</h2>
          <div className="grid md:grid-cols-3 gap-8">
            {features.map((feature) => (
              <div key={feature.title} className="p-6 bg-zinc-900 rounded-xl border border-zinc-800">
                <feature.icon className="w-10 h-10 text-primary-500 mb-4" />
                <h3 className="text-xl font-semibold mb-2">{feature.title}</h3>
                <p className="text-zinc-400">{feature.description}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* Documentation */}
      <section className="py-24 px-6 bg-zinc-900/30">
        <div className="max-w-6xl mx-auto">
          <h2 className="text-3xl font-bold text-center mb-12">Documentation</h2>
          <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
            {sections.map((section) => (
              <Link
                key={section.href}
                href={section.href}
                className="p-6 bg-zinc-900 rounded-xl border border-zinc-800 hover:border-primary-500/50 transition group"
              >
                <section.icon className="w-8 h-8 text-primary-500 mb-3 group-hover:scale-110 transition" />
                <h3 className="text-lg font-semibold mb-1">{section.title}</h3>
                <p className="text-zinc-400 text-sm">{section.description}</p>
              </Link>
            ))}
          </div>
        </div>
      </section>

      {/* Footer */}
      <footer className="border-t border-zinc-800 py-12 px-6">
        <div className="max-w-6xl mx-auto text-center text-zinc-500">
          <p>ZAP Protocol - MIT License</p>
          <div className="flex items-center justify-center gap-6 mt-4">
            <a href="https://github.com/zap-protocol/zap" className="hover:text-white transition">
              Core
            </a>
            <a href="https://github.com/zap-protocol/zap-ts" className="hover:text-white transition">
              TypeScript
            </a>
            <a href="https://github.com/zap-protocol/zap-py" className="hover:text-white transition">
              Python
            </a>
            <a href="https://github.com/zap-protocol/zap-cpp" className="hover:text-white transition">
              C++
            </a>
          </div>
        </div>
      </footer>
    </div>
  )
}

'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { Zap, Package, BookOpen, Code, Server, FileCode, ChevronRight } from 'lucide-react'

const navigation = [
  { href: '/installation/', title: 'Installation', icon: Package },
  { href: '/quickstart/', title: 'Quick Start', icon: Zap },
  { href: '/client/', title: 'Client API', icon: Code },
  { href: '/gateway/', title: 'Gateway', icon: Server },
  { href: '/types/', title: 'Types', icon: FileCode },
  { href: '/examples/', title: 'Examples', icon: BookOpen },
]

export default function DocLayout({
  children,
  title,
  description,
}: {
  children: React.ReactNode
  title: string
  description?: string
}) {
  const pathname = usePathname()

  return (
    <div className="min-h-screen flex flex-col">
      {/* Header */}
      <header className="border-b border-zinc-800 sticky top-0 bg-black/90 backdrop-blur z-50">
        <div className="max-w-7xl mx-auto px-6 py-4 flex items-center justify-between">
          <Link href="/" className="flex items-center gap-3">
            <Zap className="w-7 h-7 text-primary-500" />
            <span className="text-lg font-semibold">ZAP Go</span>
          </Link>
          <nav className="flex items-center gap-6">
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

      <div className="flex-1 flex">
        {/* Sidebar */}
        <aside className="w-64 border-r border-zinc-800 p-6 hidden md:block">
          <nav className="space-y-1">
            {navigation.map((item) => {
              const isActive = pathname === item.href || pathname === item.href.slice(0, -1)
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={`flex items-center gap-3 px-3 py-2 rounded-lg transition ${
                    isActive
                      ? 'bg-primary-500/10 text-primary-500'
                      : 'text-zinc-400 hover:text-white hover:bg-zinc-800'
                  }`}
                >
                  <item.icon className="w-5 h-5" />
                  {item.title}
                </Link>
              )
            })}
          </nav>
        </aside>

        {/* Main content */}
        <main className="flex-1 p-8 max-w-4xl">
          {/* Breadcrumb */}
          <div className="flex items-center gap-2 text-sm text-zinc-500 mb-6">
            <Link href="/" className="hover:text-white transition">
              Home
            </Link>
            <ChevronRight className="w-4 h-4" />
            <span className="text-white">{title}</span>
          </div>

          {/* Title */}
          <div className="mb-8">
            <h1 className="text-4xl font-bold mb-2">{title}</h1>
            {description && <p className="text-xl text-zinc-400">{description}</p>}
          </div>

          {/* Content */}
          <div className="prose prose-invert prose-zinc max-w-none">
            {children}
          </div>
        </main>
      </div>
    </div>
  )
}

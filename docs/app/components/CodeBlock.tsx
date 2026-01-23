'use client'

import { useState } from 'react'
import { Check, Copy } from 'lucide-react'

interface CodeBlockProps {
  code: string
  language?: string
  filename?: string
}

export default function CodeBlock({ code, language = 'go', filename }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(code)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="relative group my-6">
      {filename && (
        <div className="bg-zinc-800 px-4 py-2 rounded-t-lg border-b border-zinc-700 text-sm text-zinc-400 font-mono">
          {filename}
        </div>
      )}
      <div className={`relative ${filename ? 'rounded-b-lg' : 'rounded-lg'}`}>
        <pre className={`bg-zinc-900 p-4 overflow-x-auto ${filename ? 'rounded-t-none' : ''}`}>
          <code className={`language-${language}`}>{code}</code>
        </pre>
        <button
          onClick={handleCopy}
          className="absolute top-3 right-3 p-2 bg-zinc-800 rounded-lg opacity-0 group-hover:opacity-100 transition hover:bg-zinc-700"
          title="Copy code"
        >
          {copied ? (
            <Check className="w-4 h-4 text-green-500" />
          ) : (
            <Copy className="w-4 h-4 text-zinc-400" />
          )}
        </button>
      </div>
    </div>
  )
}

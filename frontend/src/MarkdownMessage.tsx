import { Clipboard } from '@wailsio/runtime'
import { isValidElement, useEffect, useRef, useState, type ReactNode } from 'react'
import { Check, Copy } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import rehypeHighlight from 'rehype-highlight'
import remarkGfm from 'remark-gfm'

function markdownText(children: ReactNode): string {
  if (typeof children === 'string' || typeof children === 'number') return String(children)
  if (Array.isArray(children)) return children.map(markdownText).join('')
  if (isValidElement<{ children?: ReactNode }>(children)) return markdownText(children.props.children)
  return ''
}

function MarkdownMessage({ body }: { body: string }) {
  const [copiedCode, setCopiedCode] = useState('')
  const [copyError, setCopyError] = useState('')
  const copyResetTimeout = useRef<number | null>(null)

  useEffect(() => () => {
    if (copyResetTimeout.current !== null) window.clearTimeout(copyResetTimeout.current)
  }, [])

  async function copyCode(code: string) {
    try {
      await Clipboard.SetText(code)
      setCopyError('')
      setCopiedCode(code)
      if (copyResetTimeout.current !== null) window.clearTimeout(copyResetTimeout.current)
      copyResetTimeout.current = window.setTimeout(() => setCopiedCode(''), 1400)
    } catch (copyFailure) {
      setCopyError(copyFailure instanceof Error ? copyFailure.message : String(copyFailure))
    }
  }

  return (
    <div className="message-body markdown-message">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[[rehypeHighlight, { detect: true }]]}
        components={{
          a({ children, href }) { return <a href={href} target="_blank" rel="noreferrer">{children}</a> },
          pre({ children }) {
            const code = markdownText(children).replace(/\n$/, '')
            return <div className="markdown-code">
              <div className="markdown-code-toolbar">
                <span>Code</span>
                <button className="code-copy" type="button" aria-label={copiedCode === code ? 'Code copied' : 'Copy code'} title="Copy code" onClick={() => void copyCode(code)}>
                  {copiedCode === code ? <Check size={13} /> : <Copy size={13} />}{copiedCode === code ? 'Copied' : 'Copy code'}
                </button>
              </div>
              <pre>{children}</pre>
              {copyError && <span className="code-copy-error" role="status">{copyError}</span>}
            </div>
          },
        }}
      >
        {body}
      </ReactMarkdown>
    </div>
  )
}

export default MarkdownMessage
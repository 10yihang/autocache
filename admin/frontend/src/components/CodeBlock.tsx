import './CodeBlock.css'

interface CodeBlockProps {
  children: string
  lang?: string
}

export function CodeBlock({ children, lang }: CodeBlockProps) {
  return (
    <div className="code-block">
      {lang && <span className="code-block__lang">{lang}</span>}
      <pre className="code-block__pre">{children}</pre>
    </div>
  )
}

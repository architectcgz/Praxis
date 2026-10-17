import { isDiffContent, DiffCode, MessageMarkdown } from './MarkdownViews'

export function ToolResultContent({ content }: { content: string }) {
    if (isDiffContent(content)) return <DiffCode content={content} />
    if (content.includes('```') || /(?:^|\n)\s{0,3}(?:#{1,6}\s|>\s|[-*+]\s|\d+\.\s)/.test(content)) return <MessageMarkdown content={content} />
    return <pre className="output-text">{content}</pre>
}

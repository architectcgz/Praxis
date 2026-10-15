import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'
import { isDiffContent } from './MarkdownViews'
import { OutputStatus } from './OutputStatus'
import { PatchCommand } from './PatchViews'
import type { ToolPresentation } from './toolPresentation'

export function TerminalOutput({ tool, duration, approval }: { tool: ToolPresentation; duration?: ReactNode; approval?: ReactNode }) {
    const [expanded, setExpanded] = useState(false)
    const [canExpandCommand, setCanExpandCommand] = useState(/[\r\n]/.test(tool.summary))
    const commandRef = useRef<HTMLElement>(null)
    useLayoutEffect(() => {
        const command = commandRef.current
        if (!command) return
        const update = () => {
            const bar = command.parentElement
            const chevron = bar?.querySelector('.output-terminal-chevron')
            // 箭头本身会挤占宽度，判断时把它的空间还给命令，避免临界宽度下误判。
            const availableWidth = command.clientWidth + (chevron && bar ? chevron.getBoundingClientRect().width + parseFloat(getComputedStyle(bar).columnGap) : 0)
            setCanExpandCommand(/[\r\n]/.test(tool.summary) || command.scrollWidth > availableWidth + 1)
        }
        update()
        const observer = new ResizeObserver(update)
        observer.observe(command)
        return () => observer.disconnect()
    }, [tool.summary, tool.status, canExpandCommand])
    const lines = tool.content.trim() ? tool.content.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n') : []
    const shown = expanded ? lines : lines.slice(0, 6)
    const running = tool.status === 'running'
    const commandPreview = tool.summary.split(/\r?\n/)[0]
    const commandHeading = <>
        <code ref={commandRef} className="output-terminal-command">{commandPreview}</code>
        {canExpandCommand && <ChevronDown className="output-terminal-chevron" size={14} aria-hidden="true" />}
    </>
    return <article className="output-terminal" data-status={tool.status} aria-label={`工具调用 · ${tool.summary}`}>
        <details className="output-fold output-terminal-details" open={tool.status === 'running' || tool.status === 'failed' || tool.status === 'timeout' || tool.status === 'awaiting_approval' ? true : undefined}>
            <summary className="output-fold-button output-tool-header">
                <span className="output-tool-summary"><strong>{tool.name || 'bash'}</strong><code>{commandPreview}</code></span>
                <OutputStatus status={tool.status} label={tool.notice.startsWith('搜索无结果') ? '无匹配' : undefined} />
                {duration}
                <ChevronDown className="output-chevron" size={14} aria-hidden="true" />
            </summary>
            <div className="output-tool-body">
                <section className="output-section output-input">
                    <div className="output-section-label">调用参数</div>
                    {tool.status === 'awaiting_approval' ? <pre className="output-terminal-command-full" tabIndex={0} aria-label="待执行命令">{tool.summary}</pre> : canExpandCommand ? <details className="output-terminal-command-fold">
                        <summary className="output-terminal-bar" aria-label="展开或收起命令">{commandHeading}</summary>
                        <pre className="output-terminal-command-full">{isDiffContent(tool.summary) ? <PatchCommand content={tool.summary} /> : tool.summary}</pre>
                    </details> : <div className="output-terminal-bar output-terminal-command-static">{commandHeading}</div>}
                    {typeof tool.input.path === 'string' && <div className="output-terminal-path" title={tool.input.path}>工作目录 <code>{tool.input.path}</code></div>}
                </section>
                {!approval && <section className="output-section output-result">
                    <div className="output-section-label">执行输出</div>
                    <div className="output-terminal-scroll" tabIndex={0} aria-label="命令输出">
                        {shown.length > 0 ? shown.map((line, index) => <div className="output-terminal-line" key={index}>{line || ' '}</div>)
                            : running ? <p className="output-terminal-empty">等待工具返回输出…</p> : <p className="output-terminal-empty">暂无输出</p>}
                    </div>
                    {lines.length > shown.length && <div className="output-terminal-more">显示 {shown.length} / {lines.length} 行 · 更多输出已折叠</div>}
                    {lines.length > 6 && <div className="output-terminal-foot">
                        <button type="button" className="output-button" aria-expanded={expanded} onClick={() => setExpanded((value) => !value)}>{expanded ? '收起日志' : '展开全部'}</button>
                    </div>}
                </section>}
            </div>
        </details>
        {approval}
    </article>
}

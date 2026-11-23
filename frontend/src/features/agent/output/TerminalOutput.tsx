import { useState, type ReactNode } from 'react'
import { Terminal } from 'lucide-react'
import { CopyIconButton } from './CopyIconButton'
import { isDiffContent } from './MarkdownViews'
import { PatchCommand } from './PatchViews'
import { ActivityItem, activityTone, isOpenByDefault } from './ActivityLedger'
import { formatByteSize, type ToolPresentation } from './toolPresentation'

/** 单行且不超过这个长度且未切换工作目录的命令在账本行里已完整可见，展开区不再重复展示。 */
const inlineCommandLimit = 64
const previewLineLimit = 16

/** Bash 调用：方案 A 统一细线账本的一行，展开后为紧凑命令栏与终端输出。 */
export function TerminalOutput({ tool, duration }: { tool: ToolPresentation; duration?: ReactNode }) {
    const [expanded, setExpanded] = useState(false)
    const lines = tool.content.trim() ? tool.content.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n') : []
    const shown = expanded ? lines : lines.slice(0, previewLineLimit)
    const running = tool.status === 'running'
    const commandPreview = tool.summary.split(/\r?\n/)[0]
    const awaitingApproval = tool.status === 'awaiting_approval'
    const singleLine = !/[\r\n]/.test(tool.summary)
    const workdir = typeof tool.input.path === 'string' && tool.input.path !== '.' ? tool.input.path : ''
    const showCommand = awaitingApproval || !singleLine || tool.summary.length > inlineCommandLimit || Boolean(workdir)
    const outputFile = typeof tool.result.outputFile === 'string' ? tool.result.outputFile : ''
    const outputSize = typeof tool.result.size === 'number' ? tool.result.size : undefined

    const metrics = (
        <>
            {tool.exitCode !== undefined && tool.exitCode !== 0 && tool.status !== 'info' && (
                <span className="act-metric-pill is-bad">退出码 {tool.exitCode}</span>
            )}
            {tool.status === 'info' && tool.notice.startsWith('搜索无结果') && (
                <span className="act-metric-pill">无匹配</span>
            )}
            {outputFile ? (
                <span className="act-metric-pill">
                    已转存{outputSize !== undefined ? ` ${formatByteSize(outputSize)}` : ''}
                </span>
            ) : lines.length > 0 && (
                <span className="act-metric-pill">{lines.length} 行</span>
            )}
        </>
    )

    return (
        <ActivityItem
            tone={activityTone(tool.status)}
            icon={<Terminal size={13} strokeWidth={1.85} aria-hidden="true" />}
            name={tool.label || '终端'}
            rawName={tool.name || 'bash'}
            argument={commandPreview}
            metrics={metrics}
            status={tool.status}
            statusLabel={tool.notice.startsWith('搜索无结果') ? '无匹配' : undefined}
            duration={duration}
            open={isOpenByDefault(tool.status)}
            foot={(lines.length > 0 || tool.exitCode !== undefined || workdir || outputFile || tool.notice) ? (
                <>
                    {tool.exitCode !== undefined && (
                        <span className={tool.exitCode !== 0 ? 'instrument-foot-bad' : undefined}>退出码 {tool.exitCode}</span>
                    )}
                    {lines.length > 0 && <span>{lines.length} 行输出</span>}
                    {workdir && <span>工作目录 {workdir}</span>}
                    {outputFile && <span>输出文件 {outputFile}</span>}
                    {tool.notice && <span>{tool.notice}</span>}
                </>
            ) : undefined}
        >
            {showCommand && (
                <div className="output-command-bar">
                    <pre className="output-command" tabIndex={0} aria-label={awaitingApproval ? '待执行命令' : '完整命令'}>
                        <span className="output-command-prompt" aria-hidden="true">$</span>
                        {isDiffContent(tool.summary) ? <PatchCommand content={tool.summary} /> : tool.summary}
                    </pre>
                    <div className="output-command-bar-right">
                        {workdir && <span className="output-command-dir" title={workdir}>{workdir}</span>}
                        <CopyIconButton
                            className="instrument-copy-button"
                            copiedLabel="命令已复制"
                            label="复制命令"
                            getText={() => tool.summary}
                        />
                    </div>
                </div>
            )}
            {!awaitingApproval && (
                <section className="output-section">
                    {lines.length > 0 && (
                        <div className="output-section-head">
                            <span className="output-section-label">输出</span>
                            <CopyIconButton
                                className="instrument-copy-button"
                                copiedLabel="输出已复制"
                                label="复制命令输出"
                                getText={() => tool.content}
                            />
                        </div>
                    )}
                    <div className="output-terminal-scroll" tabIndex={0} aria-label="命令输出">
                        {shown.length > 0
                            ? shown.map((line, index) => <div className="output-terminal-line" key={index}>{line || ' '}</div>)
                            : running
                                ? <p className="output-muted">等待工具返回输出…</p>
                                : <p className="output-muted">暂无输出</p>}
                    </div>
                    {lines.length > shown.length && (
                        <div className="output-muted">显示 {shown.length} / {lines.length} 行 · 其余输出已折叠</div>
                    )}
                    {lines.length > previewLineLimit && (
                        <div className="output-section-actions">
                            <button
                                type="button"
                                className="output-button"
                                aria-expanded={expanded}
                                onClick={() => setExpanded((value) => !value)}
                            >
                                {expanded ? '收起日志' : `展开全部（${lines.length} 行）`}
                            </button>
                        </div>
                    )}
                </section>
            )}
        </ActivityItem>
    )
}

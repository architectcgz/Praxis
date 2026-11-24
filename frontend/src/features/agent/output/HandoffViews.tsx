import type { ReactNode } from 'react'
import {
    ArrowUpRight,
    Ban,
    Check,
    ChevronRight,
    CircleAlert,
    Clock,
    CornerDownRight,
    ExternalLink,
    Inbox,
    LoaderCircle,
    Send,
} from 'lucide-react'
import { CopyIconButton } from './CopyIconButton'
import { MessageMarkdown } from './MarkdownViews'
import { statusLabels } from './OutputStatus'
import type { ToolPresentation } from './toolPresentation'

const HANDOFF_TOOL_NAMES = new Set([
    'subagent_start',
    'subagent_wait',
    'send_to_subagent',
    'subagent_cancel',
    'report_to_parent',
])

export function isHandoffTool(name?: string): boolean {
    return Boolean(name && HANDOFF_TOOL_NAMES.has(name))
}

export type ParsedSubagentReport = {
    reportId: string
    agentId: string
    runId: string
    outcome: string
    final: boolean
    partial: boolean
    failure: string
    body: string
}

const subagentReportPattern = /^\[Subagent report ([^\]]+)\]\s+agent=([^\s]+)\s+run=([^\s]+)\s+outcome=([^\s]+)\s+final=(true|false)\s+partial=(true|false)\s+failure=([^\r\n]*)(?:\r?\n([\s\S]*))?$/

/** 解析后端写入父线程的子代理报告协议头，无法匹配时返回 null。 */
export function parseSubagentReport(content: string): ParsedSubagentReport | null {
    const match = content.match(subagentReportPattern)
    if (!match) return null
    return {
        reportId: match[1],
        agentId: match[2],
        runId: match[3],
        outcome: match[4],
        final: match[5] === 'true',
        partial: match[6] === 'true',
        failure: match[7].trim(),
        body: (match[8] ?? '').trim(),
    }
}

export type AgentDirectoryEntry = {
    name: string
    active: boolean
}

export type AgentDirectory = Record<string, AgentDirectoryEntry>

function resolveAgentName(id: string, agentDirectory?: AgentDirectory): string {
    return (id && agentDirectory?.[id]?.name) || id || '子代理'
}

function isAgentActive(id: string, agentDirectory?: AgentDirectory): boolean {
    return Boolean(id && agentDirectory?.[id]?.active)
}

function reportStatusMeta(report: ParsedSubagentReport): { label: string; tone: 'done' | 'accent' | 'warn' | 'fail' } {
    if (report.outcome === 'failed' || report.failure) return { label: '执行失败', tone: 'fail' }
    if (report.outcome === 'interrupted') return { label: '已中断', tone: 'fail' }
    if (report.partial) return { label: '部分结果', tone: 'warn' }
    if (report.final) return { label: '最终交付', tone: 'done' }
    return { label: '进展汇报', tone: 'accent' }
}

function previewPlainText(markdown: string): string {
    return markdown
        .replace(/```[\s\S]*?```/g, '[代码片段]')
        .replace(/[#>*`]/g, '')
        .replace(/\s+/g, ' ')
        .trim()
}

/** 子代理回传主代理的汇报卡片：统一 1px 细边框、解析协议头、支持折叠；仅仍在运行的子代理提供跳转按钮。 */
export function SubagentReportCard({
    report,
    createdAt,
    formattedTime,
    agentDirectory,
    onOpenSubagent,
}: {
    report: ParsedSubagentReport
    createdAt: string
    formattedTime: string
    agentDirectory?: AgentDirectory
    onOpenSubagent: (id: string) => void
}) {
    const agentName = resolveAgentName(report.agentId, agentDirectory)
    const status = reportStatusMeta(report)
    const isFailure = status.tone === 'fail'
    const lineCount = report.body ? report.body.split(/\r?\n/).length : 0
    const openByDefault = isFailure || (report.body.length <= 420 && lineCount <= 6)
    const preview = previewPlainText(report.body)
    const canInspect = !report.final && isAgentActive(report.agentId, agentDirectory)

    return (
        <article className="message handoff-message">
            <div className={`handoff-card is-incoming ${isFailure ? 'is-fail' : ''}`}>
                <details className="handoff-details" open={openByDefault || undefined}>
                    <summary className="handoff-summary">
                        <div className="handoff-route">
                            <ChevronRight className="handoff-chev" size={13} strokeWidth={2} aria-hidden="true" />
                            <span className={`handoff-pill is-${status.tone}`}>
                                <Inbox size={12} strokeWidth={1.9} aria-hidden="true" />
                                <span>子代理汇报</span>
                            </span>
                            <span className="handoff-agent-name" title={report.agentId}>{agentName}</span>
                            <span className={`handoff-tag is-${status.tone}`}>{status.label}</span>
                        </div>
                        <div className="handoff-actions">
                            <time className="handoff-time" dateTime={createdAt} title={createdAt}>{formattedTime}</time>
                            {canInspect && (
                                <button
                                    className="handoff-jump"
                                    type="button"
                                    onClick={(event) => {
                                        event.preventDefault()
                                        event.stopPropagation()
                                        onOpenSubagent(report.agentId)
                                    }}
                                >
                                    <span>查看子代理</span>
                                    <ExternalLink size={11} strokeWidth={1.9} aria-hidden="true" />
                                </button>
                            )}
                        </div>
                    </summary>

                    {preview && <div className="handoff-preview">{preview}</div>}

                    <div className="handoff-report-body">
                        {report.failure && (
                            <div className="message-error-box handoff-error-inline" role="alert">
                                <CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" />
                                <span>异常码：{report.failure}</span>
                            </div>
                        )}
                        {report.body ? (
                            <MessageMarkdown content={report.body} />
                        ) : (
                            <p className="output-muted">本次汇报没有附带正文内容。</p>
                        )}
                        {report.body && (
                            <div className="handoff-report-foot">
                                <span className="handoff-report-id">报告 {report.reportId}</span>
                                <CopyIconButton
                                    className="message-copy-button"
                                    copiedLabel="汇报内容已复制"
                                    label="复制汇报内容"
                                    getText={() => report.body}
                                />
                            </div>
                        )}
                    </div>
                </details>
            </div>
        </article>
    )
}

/** 子代理线程中收到的主代理委派任务 / 补充指令卡片，与人类用户右置气泡清晰区分。 */
export function AgentDirectiveCard({
    content,
    createdAt,
    formattedTime,
}: {
    content: string
    createdAt: string
    formattedTime: string
}) {
    return (
        <article className="message handoff-message">
            <div className="handoff-card is-directive">
                <div className="handoff-head">
                    <div className="handoff-route">
                        <span className="handoff-pill is-accent">
                            <CornerDownRight size={12} strokeWidth={1.9} aria-hidden="true" />
                            <span>来自主代理的委派</span>
                        </span>
                    </div>
                    <div className="handoff-actions">
                        <time className="handoff-time" dateTime={createdAt} title={createdAt}>{formattedTime}</time>
                        {content && (
                            <CopyIconButton
                                className="message-copy-button"
                                copiedLabel="委派内容已复制"
                                label="复制委派内容"
                                getText={() => content}
                            />
                        )}
                    </div>
                </div>
                <div className="handoff-body">
                    {content ? <MessageMarkdown content={content} /> : <p className="output-muted">无指令正文</p>}
                </div>
            </div>
        </article>
    )
}

/** 渲染 5 类代理协作工具（委派、补充指令、等待、取消、向主代理汇报），统一 1px 细边框风格。 */
export function HandoffToolCard({
    tool,
    duration,
    agentDirectory,
    onOpenSubagent,
}: {
    tool: ToolPresentation
    duration?: ReactNode
    agentDirectory?: AgentDirectory
    onOpenSubagent?: (id: string) => void
}) {
    if (tool.name === 'subagent_wait') {
        return <SubagentWaitStrip tool={tool} duration={duration} agentDirectory={agentDirectory} onOpenSubagent={onOpenSubagent} />
    }
    if (tool.name === 'subagent_cancel') {
        return <SubagentCancelStrip tool={tool} duration={duration} agentDirectory={agentDirectory} onOpenSubagent={onOpenSubagent} />
    }
    if (tool.name === 'send_to_subagent') {
        return <SendToSubagentCard tool={tool} duration={duration} agentDirectory={agentDirectory} onOpenSubagent={onOpenSubagent} />
    }
    if (tool.name === 'report_to_parent') {
        return <ReportToParentCard tool={tool} duration={duration} />
    }
    return <SubagentStartCard tool={tool} duration={duration} agentDirectory={agentDirectory} onOpenSubagent={onOpenSubagent} />
}

function SubagentStartCard({
    tool,
    duration,
    agentDirectory,
    onOpenSubagent,
}: {
    tool: ToolPresentation
    duration?: ReactNode
    agentDirectory?: AgentDirectory
    onOpenSubagent?: (id: string) => void
}) {
    const task = typeof tool.input.task === 'string' ? tool.input.task.trim() : ''
    const allowedTools = Array.isArray(tool.input.allowedTools)
        ? tool.input.allowedTools.filter((item): item is string => typeof item === 'string')
        : []
    const childId = typeof tool.result.agentId === 'string' ? tool.result.agentId : ''
    const agentName = childId ? resolveAgentName(childId, agentDirectory) : ''
    const canInspect = Boolean(childId && onOpenSubagent && isAgentActive(childId, agentDirectory))
    const failed = tool.status === 'failed' || tool.status === 'timeout'
    const live = tool.status === 'running'
    const errorMessage = failed ? (typeof tool.result.error === 'string' ? tool.result.error : tool.content || tool.notice) : ''

    return (
        <div className={`handoff-card ${failed ? 'is-fail' : live ? 'is-live' : ''}`}>
            <div className="handoff-head">
                <div className="handoff-route">
                    <span className={`handoff-pill ${failed ? 'is-fail' : live ? 'is-live' : 'is-accent'}`}>
                        {live
                            ? <LoaderCircle className="is-spinning" size={12} aria-hidden="true" />
                            : <ArrowUpRight size={12} strokeWidth={2} aria-hidden="true" />}
                        <span>委派子代理</span>
                    </span>
                    {agentName && <span className="handoff-agent-name" title={childId}>{agentName}</span>}
                    <span className={`handoff-tag ${failed ? 'is-fail' : live ? 'is-live' : 'is-done'}`}>
                        {failed ? '启动失败' : live ? '启动中' : '已派发'}
                    </span>
                </div>
                <div className="handoff-actions">
                    {duration && <span className="handoff-time">{duration}</span>}
                    {canInspect && onOpenSubagent && (
                        <button className="handoff-jump" type="button" onClick={() => onOpenSubagent(childId)}>
                            <span>查看子代理</span>
                            <ExternalLink size={11} strokeWidth={1.9} aria-hidden="true" />
                        </button>
                    )}
                </div>
            </div>
            <div className="handoff-body">
                {task && <p className="handoff-task-text">{task}</p>}
                {errorMessage && (
                    <div className="message-error-box handoff-error-inline" role="alert">
                        <CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" />
                        <span>{errorMessage}</span>
                    </div>
                )}
                {allowedTools.length > 0 && (
                    <div className="handoff-meta-row">
                        <div className="handoff-chips">
                            <span className="handoff-meta-label">授权工具</span>
                            {allowedTools.map((item) => <code className="handoff-chip" key={item}>{item}</code>)}
                        </div>
                    </div>
                )}
            </div>
        </div>
    )
}

function SendToSubagentCard({
    tool,
    duration,
    agentDirectory,
    onOpenSubagent,
}: {
    tool: ToolPresentation
    duration?: ReactNode
    agentDirectory?: AgentDirectory
    onOpenSubagent?: (id: string) => void
}) {
    const targetId = typeof tool.input.id === 'string' ? tool.input.id : ''
    const message = typeof tool.input.message === 'string' ? tool.input.message.trim() : ''
    const canInspect = Boolean(targetId && onOpenSubagent && isAgentActive(targetId, agentDirectory))
    const failed = tool.status === 'failed' || tool.status === 'timeout'
    const live = tool.status === 'running'
    const errorMessage = failed ? (typeof tool.result.error === 'string' ? tool.result.error : tool.content || tool.notice) : ''

    return (
        <div className={`handoff-card ${failed ? 'is-fail' : live ? 'is-live' : ''}`}>
            <div className="handoff-head">
                <div className="handoff-route">
                    <span className={`handoff-pill ${failed ? 'is-fail' : 'is-accent'}`}>
                        <Send size={11} strokeWidth={1.9} aria-hidden="true" />
                        <span>补充指令</span>
                    </span>
                    {targetId && <span className="handoff-agent-name" title={targetId}>{resolveAgentName(targetId, agentDirectory)}</span>}
                    <span className={`handoff-tag ${failed ? 'is-fail' : live ? 'is-live' : 'is-done'}`}>
                        {failed ? '投递失败' : live ? '投递中' : '已投递'}
                    </span>
                </div>
                <div className="handoff-actions">
                    {duration && <span className="handoff-time">{duration}</span>}
                    {canInspect && onOpenSubagent && (
                        <button className="handoff-jump" type="button" onClick={() => onOpenSubagent(targetId)}>
                            <span>查看子代理</span>
                            <ExternalLink size={11} strokeWidth={1.9} aria-hidden="true" />
                        </button>
                    )}
                </div>
            </div>
            <div className="handoff-body">
                {message && <p className="handoff-task-text">{message}</p>}
                {errorMessage && (
                    <div className="message-error-box handoff-error-inline" role="alert">
                        <CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" />
                        <span>{errorMessage}</span>
                    </div>
                )}
            </div>
        </div>
    )
}

function ReportToParentCard({ tool, duration }: { tool: ToolPresentation; duration?: ReactNode }) {
    const message = typeof tool.input.message === 'string' ? tool.input.message.trim() : ''
    const failed = tool.status === 'failed' || tool.status === 'timeout'
    const live = tool.status === 'running'
    const errorMessage = failed ? (typeof tool.result.error === 'string' ? tool.result.error : tool.content || tool.notice) : ''

    return (
        <div className={`handoff-card is-incoming ${failed ? 'is-fail' : live ? 'is-live' : ''}`}>
            <div className="handoff-head">
                <div className="handoff-route">
                    <span className={`handoff-pill ${failed ? 'is-fail' : 'is-done'}`}>
                        <ArrowUpRight size={12} strokeWidth={2} aria-hidden="true" />
                        <span>向主代理汇报</span>
                    </span>
                    <span className={`handoff-tag ${failed ? 'is-fail' : live ? 'is-live' : 'is-done'}`}>
                        {failed ? '汇报失败' : live ? '投递中' : '已接收'}
                    </span>
                </div>
                <div className="handoff-actions">
                    {duration && <span className="handoff-time">{duration}</span>}
                </div>
            </div>
            <div className="handoff-body">
                {message ? <MessageMarkdown content={message} /> : <p className="output-muted">无汇报正文</p>}
                {errorMessage && (
                    <div className="message-error-box handoff-error-inline" role="alert">
                        <CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" />
                        <span>{errorMessage}</span>
                    </div>
                )}
            </div>
        </div>
    )
}

type WaitReceipt = {
    agentId: string
    outcome?: string
    final?: boolean
}

function parseWaitReceipts(content: string): WaitReceipt[] {
    if (!content.trim().startsWith('[')) return []
    try {
        const parsed = JSON.parse(content) as unknown
        if (!Array.isArray(parsed)) return []
        return parsed.flatMap((item) => {
            if (typeof item !== 'object' || item === null) return []
            const record = item as Record<string, unknown>
            if (typeof record.agentId !== 'string') return []
            return [{
                agentId: record.agentId,
                outcome: typeof record.outcome === 'string' ? record.outcome : undefined,
                final: typeof record.final === 'boolean' ? record.final : undefined,
            }]
        })
    } catch {
        return []
    }
}

function receiptOutcomeLabel(receipt?: WaitReceipt): { label: string; dotClass: string } | null {
    if (!receipt) return null
    if (receipt.outcome === 'failed' || receipt.outcome === 'interrupted') return { label: '异常', dotClass: 'is-fail' }
    if (receipt.final) return { label: '已完成', dotClass: 'is-done' }
    return { label: '有进展', dotClass: 'is-accent' }
}

function SubagentWaitStrip({
    tool,
    duration,
    agentDirectory,
    onOpenSubagent,
}: {
    tool: ToolPresentation
    duration?: ReactNode
    agentDirectory?: AgentDirectory
    onOpenSubagent?: (id: string) => void
}) {
    const ids = Array.isArray(tool.input.ids) ? tool.input.ids.filter((item): item is string => typeof item === 'string') : []
    const modeLabel = tool.input.mode === 'all' ? '全部完成' : '任一更新'
    const receipts = parseWaitReceipts(tool.content)
    const receiptByAgent = new Map(receipts.map((item) => [item.agentId, item]))
    const failed = tool.status === 'failed' || tool.status === 'timeout'
    const live = tool.status === 'running'
    const errorMessage = failed ? (typeof tool.result.error === 'string' ? tool.result.error : tool.content || tool.notice) : ''

    return (
        <div className={`handoff-strip ${failed ? 'is-fail' : live ? 'is-live' : ''}`}>
            <div className="handoff-strip-main">
                <div className="handoff-strip-left">
                    {live ? (
                        <LoaderCircle className="is-spinning handoff-strip-icon" size={13} aria-hidden="true" />
                    ) : failed ? (
                        <CircleAlert className="handoff-strip-icon" size={13} aria-hidden="true" />
                    ) : (
                        <Clock className="handoff-strip-icon" size={13} aria-hidden="true" />
                    )}
                    <span className="handoff-strip-label">
                        {live ? `等待子代理（${modeLabel}）` : `已汇合子代理回执（${modeLabel}）`}
                    </span>
                    <div className="handoff-strip-pills">
                        {ids.map((id) => {
                            const receipt = receiptByAgent.get(id)
                            const meta = receiptOutcomeLabel(receipt)
                            const name = resolveAgentName(id, agentDirectory)
                            const canInspect = Boolean(onOpenSubagent && isAgentActive(id, agentDirectory))
                            const dotClass = meta ? meta.dotClass : live ? 'is-live' : 'is-done'
                            return canInspect && onOpenSubagent ? (
                                <button
                                    className="handoff-agent-pill"
                                    type="button"
                                    key={id}
                                    title={`查看 ${name}`}
                                    onClick={() => onOpenSubagent(id)}
                                >
                                    <span className={`deck-dot ${dotClass}`} aria-hidden="true" />
                                    <span>{name}</span>
                                    {meta && <span className="handoff-agent-pill-state">{meta.label}</span>}
                                </button>
                            ) : (
                                <span className="handoff-agent-pill" key={id}>
                                    <span className={`deck-dot ${dotClass}`} aria-hidden="true" />
                                    <span>{name}</span>
                                    {meta && <span className="handoff-agent-pill-state">{meta.label}</span>}
                                </span>
                            )
                        })}
                    </div>
                </div>
                <div className="handoff-strip-right">
                    <span className="handoff-time">{duration || statusLabels[tool.status]}</span>
                    {!live && !failed && <Check className="handoff-strip-done" size={13} aria-hidden="true" />}
                </div>
            </div>
            {errorMessage && (
                <div className="message-error-box handoff-error-inline" role="alert">
                    <CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" />
                    <span>{errorMessage}</span>
                </div>
            )}
        </div>
    )
}

function SubagentCancelStrip({
    tool,
    duration,
    agentDirectory,
    onOpenSubagent,
}: {
    tool: ToolPresentation
    duration?: ReactNode
    agentDirectory?: AgentDirectory
    onOpenSubagent?: (id: string) => void
}) {
    const targetId = typeof tool.input.id === 'string' ? tool.input.id : ''
    const failed = tool.status === 'failed' || tool.status === 'timeout'
    const name = targetId ? resolveAgentName(targetId, agentDirectory) : '子代理'
    const canInspect = Boolean(targetId && onOpenSubagent && isAgentActive(targetId, agentDirectory))

    return (
        <div className={`handoff-strip ${failed ? 'is-fail' : ''}`}>
            <div className="handoff-strip-main">
                <div className="handoff-strip-left">
                    <Ban className="handoff-strip-icon" size={13} aria-hidden="true" />
                    <span className="handoff-strip-label">取消子代理</span>
                    {targetId && (canInspect && onOpenSubagent ? (
                        <button className="handoff-agent-pill" type="button" onClick={() => onOpenSubagent(targetId)}>
                            <span>{name}</span>
                        </button>
                    ) : (
                        <span className="handoff-agent-pill">{name}</span>
                    ))}
                </div>
                <div className="handoff-strip-right">
                    <span className="handoff-time">{duration || (failed ? '取消失败' : '已请求取消')}</span>
                </div>
            </div>
        </div>
    )
}

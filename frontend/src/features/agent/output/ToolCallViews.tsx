import { Fragment } from 'react'
import { FileText, Wrench } from 'lucide-react'
import type { AgentEvent, MessageBlock, Timing } from '../../../api'
import { CopyIconButton } from './CopyIconButton'
import { FileChanges } from './FileChanges'
import { HandoffToolCard, isHandoffTool, type AgentDirectory } from './HandoffViews'
import { TerminalOutput } from './TerminalOutput'
import { ToolResultContent } from './ToolResultContent'
import { ActivityItem, activityTone, isOpenByDefault } from './ActivityLedger'
import { formatByteSize, formatToolInput, presentTool, toolRecord, type ToolStatus } from './toolPresentation'
import { DurationLabel } from '../../timing/DurationLabel'
import { useToolApproval } from '../ToolApprovals'

export function StreamingToolCallsView({
    tools,
    pendingStatus = 'running',
    agentDirectory,
    onOpenSubagent,
}: {
    tools: AgentEvent[]
    pendingStatus?: ToolStatus
    agentDirectory?: AgentDirectory
    onOpenSubagent?: (id: string) => void
}) {
    const activities = groupToolEvents(tools)
    const segments: ({ kind: 'handoff'; item: ToolActivity } | { kind: 'tools'; items: ToolActivity[] })[] = []
    for (const activity of activities) {
        const name = activity.call?.data.name || ''
        if (isHandoffTool(name)) {
            segments.push({ kind: 'handoff', item: activity })
            continue
        }
        const last = segments.at(-1)
        if (last?.kind === 'tools') {
            last.items.push(activity)
        } else {
            segments.push({ kind: 'tools', items: [activity] })
        }
    }

    const renderActivity = (activity: ToolActivity) => (
        <ToolCard
            key={activity.key}
            name={activity.call?.data.name || ''}
            input={activity.call?.data.arguments}
            result={activity.result?.data.result}
            isError={activity.result?.data.isError}
            timing={activity.result?.data.timing || activity.started?.data.timing}
            pendingStatus={activity.approval ? 'awaiting_approval' : activity.started ? pendingStatus : 'queued'}
            runId={activity.call?.runId || activity.approval?.runId || activity.result?.runId || ''}
            callId={activity.call?.data.callId || activity.approval?.data.callId || activity.result?.data.callId}
            agentDirectory={agentDirectory}
            onOpenSubagent={onOpenSubagent}
        />
    )

    return (
        <div className="agent-activity output-activity">
            {segments.map((segment, index) => segment.kind === 'handoff' ? (
                <div className="handoff-slot" key={segment.item.key}>
                    {renderActivity(segment.item)}
                </div>
            ) : (
                <div className="acts" key={`ledger-${index}`}>
                    {segment.items.map(renderActivity)}
                </div>
            ))}
        </div>
    )
}

export type PairedToolResult = {
    text: string
    isError?: boolean
    timing?: Timing | null
}

export function ToolCallBlock({
    block,
    events,
    runId,
    pairedResult,
    agentDirectory,
    onOpenSubagent,
}: {
    block: MessageBlock
    events: AgentEvent[]
    runId: string
    pairedResult?: PairedToolResult
    agentDirectory?: AgentDirectory
    onOpenSubagent: (id: string) => void
}) {
    const liveResult = events.findLast((event): event is Extract<AgentEvent, { kind: 'tool_result' }> => event.kind === 'tool_result' && event.data.callId === block.callId)
    const started = events.findLast((event): event is Extract<AgentEvent, { kind: 'tool_started' }> => event.kind === 'tool_started' && event.data.callId === block.callId)
    const resultText = liveResult ? liveResult.data.result : pairedResult?.text
    const isError = liveResult ? liveResult.data.isError : pairedResult?.isError
    const timing = liveResult?.data.timing || started?.data.timing || pairedResult?.timing
    return (
        <ToolCard
            name={block.name || ''}
            input={block.input}
            result={resultText}
            isError={isError}
            timing={timing}
            pendingStatus={started ? 'running' : 'unknown'}
            runId={runId}
            callId={block.callId}
            agentDirectory={agentDirectory}
            onOpenSubagent={onOpenSubagent}
        />
    )
}

export function ToolResultBlock({
    block,
    runId,
    timing,
    agentDirectory,
    onOpenSubagent,
}: {
    block: MessageBlock
    runId: string
    timing?: Timing | null
    agentDirectory?: AgentDirectory
    onOpenSubagent: (id: string) => void
}) {
    return (
        <ToolCard
            name={block.name || ''}
            result={block.text || ''}
            isError={block.isError}
            timing={timing}
            pendingStatus="unknown"
            runId={runId}
            callId={block.callId}
            agentDirectory={agentDirectory}
            onOpenSubagent={onOpenSubagent}
        />
    )
}

function ToolCard({
    name,
    input,
    result,
    isError = false,
    pendingStatus = 'running',
    runId,
    callId,
    timing,
    agentDirectory,
    onOpenSubagent,
}: {
    name: string
    input?: unknown
    result?: string
    isError?: boolean
    pendingStatus?: ToolStatus
    runId: string
    callId?: string
    timing?: Timing | null
    agentDirectory?: AgentDirectory
    onOpenSubagent?: (id: string) => void
}) {
    const approval = useToolApproval(runId, callId)
    const pendingApproval = result === undefined ? approval?.pendingApproval : undefined
    // 没有计时记录时不传元素，活动账本右列会回落到状态词而不是留空。
    const duration = timing ? <DurationLabel record={timing} running={result === undefined && pendingStatus === 'running'} label="" /> : undefined
    // 所有工具都展示后端冻结参数，不能以原始模型请求替换将被批准的操作。
    const toolInput = pendingApproval ? pendingApproval.arguments : input
    const tool = presentTool(name, toolInput, result, isError, pendingStatus)
    if (pendingApproval) tool.status = 'awaiting_approval'
    const awaitingApproval = tool.status === 'awaiting_approval'

    if (isHandoffTool(name)) {
        return <HandoffToolCard tool={tool} duration={duration} agentDirectory={agentDirectory} onOpenSubagent={onOpenSubagent} />
    }
    if (name === 'bash') return <TerminalOutput tool={tool} duration={duration} />
    if (name === 'apply_patch') return <FileChanges tool={tool} duration={duration} />

    if (name === 'read_file') {
        return <ReadFileActivity tool={tool} result={result} awaitingApproval={awaitingApproval} duration={duration} />
    }

    const formattedInput = toolInput !== undefined ? formatToolInput(toolInput) : ''
    const inputRecord = toolInput !== undefined ? toolRecord(toolInput) : {}
    const hasStructuredInput = Object.keys(inputRecord).length > 0
    const resultSize = typeof tool.result.size === 'number' ? tool.result.size : undefined
    const metrics = (resultSize !== undefined || tool.truncated) ? (
        <>
            {resultSize !== undefined && <span className="act-metric-pill">{formatByteSize(resultSize)}</span>}
            {tool.truncated && <span className="act-metric-pill">已截断</span>}
        </>
    ) : undefined

    return (
        <ActivityItem
            tone={activityTone(tool.status)}
            icon={<Wrench size={13} strokeWidth={1.85} aria-hidden="true" />}
            name={tool.label || tool.name || '工具'}
            rawName={tool.name || '工具'}
            argument={tool.summary}
            metrics={metrics}
            status={tool.status}
            statusLabel={tool.notice.startsWith('搜索无结果') ? '无匹配' : undefined}
            duration={duration}
            open={isOpenByDefault(tool.status)}
            foot={(resultSize !== undefined || tool.truncated || tool.notice) ? (
                <>
                    {resultSize !== undefined && <span>{formatByteSize(resultSize)}</span>}
                    {tool.nextOffset !== undefined && <span>已截断 · 下一偏移 {tool.nextOffset}</span>}
                    {tool.notice && <span>{tool.notice}</span>}
                </>
            ) : undefined}
        >
            {toolInput !== undefined && (
                <section className="output-section output-input">
                    <div className="output-section-head">
                        <span className="output-section-label">参数</span>
                        <CopyIconButton
                            className="instrument-copy-button"
                            copiedLabel="参数已复制"
                            label="复制工具参数"
                            getText={() => formattedInput}
                        />
                    </div>
                    {hasStructuredInput ? <ToolInputFields name={name} record={inputRecord} /> : <pre className="output-text">{formattedInput}</pre>}
                </section>
            )}
            {!awaitingApproval && (
                <section className="output-section output-result">
                    <div className="output-section-head">
                        <span className="output-section-label">输出</span>
                        {tool.content && (
                            <CopyIconButton
                                className="instrument-copy-button"
                                copiedLabel="输出已复制"
                                label="复制工具输出"
                                getText={() => tool.content}
                            />
                        )}
                    </div>
                    {result === undefined
                        ? <p className="output-muted">{tool.status === 'running' ? '正在执行，等待工具返回结果…' : '暂无输出'}</p>
                        : tool.content
                            ? <ToolResultContent content={tool.content} toolName={name} />
                            : <p className="output-muted">{tool.notice || '暂无输出'}</p>}
                </section>
            )}
        </ActivityItem>
    )
}

/** read_file 专属渲染：路径与偏移已在标题行呈现，展开态直接进入带行号代码视图，不再重复渲染参数区。 */
function ReadFileActivity({
    tool,
    result,
    awaitingApproval,
    duration,
}: {
    tool: ReturnType<typeof presentTool>
    result?: string
    awaitingApproval: boolean
    duration?: ReturnType<typeof DurationLabel>
}) {
    const offset = typeof tool.input.offset === 'number' ? tool.input.offset : 0
    const limit = typeof tool.input.limit === 'number' ? tool.input.limit : undefined
    const size = typeof tool.result.size === 'number'
        ? tool.result.size
        : tool.content ? new TextEncoder().encode(tool.content).length : undefined
    const lines = tool.content ? tool.content.replace(/\r\n/g, '\n').split('\n') : []
    // offset 为 UTF-8 字节偏移；仅当从文件开头读取（offset === 0）时行号精确，避免伪造偏移行号。
    const showLineNumbers = offset === 0 && lines.length > 0 && tool.status !== 'failed'

    const metrics = (size !== undefined || tool.truncated) ? (
        <>
            {size !== undefined && <span className="act-metric-pill">{formatByteSize(size)}</span>}
            {tool.truncated && <span className="act-metric-pill">已截断</span>}
        </>
    ) : undefined

    return (
        <ActivityItem
            tone={activityTone(tool.status)}
            icon={<FileText size={13} strokeWidth={1.85} aria-hidden="true" />}
            name={tool.label || '读取'}
            rawName="read_file"
            argument={tool.summary}
            metrics={metrics}
            status={tool.status}
            duration={duration}
            open={isOpenByDefault(tool.status)}
            foot={(size !== undefined || lines.length > 0 || offset > 0 || tool.nextOffset !== undefined || tool.notice) ? (
                <>
                    {lines.length > 0 && <span>{lines.length} 行</span>}
                    {size !== undefined && <span>{formatByteSize(size)}</span>}
                    {offset > 0 && <span>偏移 {formatByteSize(offset)}</span>}
                    {limit !== undefined && offset > 0 && <span>上限 {formatByteSize(limit)}</span>}
                    {tool.nextOffset !== undefined && <span>已截断 · 下一偏移 {tool.nextOffset}</span>}
                    {tool.notice && <span>{tool.notice}</span>}
                </>
            ) : undefined}
        >
            {!awaitingApproval && (
                <section className={`output-section ${showLineNumbers ? 'output-file-numbered' : ''}`}>
                    {tool.content && (
                        <div className="output-section-head output-file-head">
                            <span className="output-section-label">{typeof tool.input.path === 'string' ? tool.input.path : '文件内容'}</span>
                            <CopyIconButton
                                className="instrument-copy-button"
                                copiedLabel="文件内容已复制"
                                label="复制文件内容"
                                getText={() => tool.content}
                            />
                        </div>
                    )}
                    {result === undefined ? (
                        <p className="output-muted">{tool.status === 'running' ? '正在读取文件…' : '暂无输出'}</p>
                    ) : !tool.content ? (
                        <p className="output-muted">{tool.notice || '暂无输出'}</p>
                    ) : showLineNumbers ? (
                        <div className="output-code-lines" tabIndex={0} aria-label="文件内容">
                            {lines.map((line, index) => (
                                <div className="output-code-row" key={index}>
                                    <span className="output-code-ln" aria-hidden="true">{index + 1}</span>
                                    <code>{line || ' '}</code>
                                </div>
                            ))}
                        </div>
                    ) : (
                        <ToolResultContent content={tool.content} toolName="read_file" />
                    )}
                </section>
            )}
        </ActivityItem>
    )
}

const inputFieldLabels: Record<string, string> = {
    path: '路径',
    offset: '起始偏移',
    limit: '读取上限',
    command: '命令',
    timeout: '超时',
    timeoutSeconds: '超时上限',
    task: '委派任务',
    allowedTools: '授权工具',
    ids: '目标代理',
    id: '代理 ID',
    mode: '等待模式',
    message: '指令内容',
    pattern: '匹配模式',
}

function ToolInputFields({ name, record }: { name: string; record: Record<string, unknown> }) {
    const entries = Object.entries(record).filter(([, value]) => value !== undefined && value !== '')
    if (entries.length === 0) return <p className="output-muted">无参数</p>
    return (
        <dl className="output-kv-grid">
            {entries.map(([key, value]) => (
                <Fragment key={key}>
                    <dt>{inputFieldLabels[key] || key}</dt>
                    <dd>{renderInputValue(name, key, value)}</dd>
                </Fragment>
            ))}
        </dl>
    )
}

function renderInputValue(toolName: string, key: string, value: unknown) {
    if (Array.isArray(value)) {
        if (value.length === 0) return <span className="output-muted">空</span>
        if (value.every((item) => typeof item === 'string' || typeof item === 'number')) {
            return (
                <span className="output-kv-chips">
                    {value.map((item, index) => <code className="output-kv-chip" key={`${String(item)}-${index}`}>{String(item)}</code>)}
                </span>
            )
        }
        return <pre className="output-text">{formatToolInput(value)}</pre>
    }
    if (typeof value === 'object' && value !== null) {
        return <pre className="output-text">{formatToolInput(value)}</pre>
    }
    if (typeof value === 'number') {
        if (key === 'timeoutSeconds' || (toolName === 'bash' && key === 'timeout')) return <span>{value} 秒</span>
        if (toolName === 'read_file' && (key === 'offset' || key === 'limit')) return <span>{value} 字节</span>
        return <code>{String(value)}</code>
    }
    if (typeof value === 'boolean') return <code>{value ? '是' : '否'}</code>
    const text = String(value)
    if (key === 'mode') return <span>{text === 'all' ? '全部完成 (all)' : text === 'any' ? '任一更新 (any)' : text}</span>
    if (key === 'task' || key === 'message') return <span className="output-kv-text">{text}</span>
    return <code>{text}</code>
}

type ToolActivity = {
    key: string
    call?: Extract<AgentEvent, { kind: 'tool_call' }>
    approval?: Extract<AgentEvent, { kind: 'tool_approval_required' }>
    started?: Extract<AgentEvent, { kind: 'tool_started' }>
    result?: Extract<AgentEvent, { kind: 'tool_result' }>
}

function groupToolEvents(events: AgentEvent[]): ToolActivity[] {
    const activities: ToolActivity[] = []
    const byCallID = new Map<string, ToolActivity>()
    events.forEach((event, index) => {
        if (event.kind !== 'tool_call' && event.kind !== 'tool_started' && event.kind !== 'tool_approval_required' && event.kind !== 'tool_result') return
        const key = `${event.runId}:${event.turnId}:${event.data.callId || `tool-${index}`}`
        let activity = byCallID.get(key)
        if (!activity) {
            activity = { key }
            byCallID.set(key, activity)
            activities.push(activity)
        }
        if (event.kind === 'tool_call') activity.call = event
        else if (event.kind === 'tool_approval_required') activity.approval = event
        else if (event.kind === 'tool_started') activity.started = event
        else activity.result = event
    })
    return activities
}

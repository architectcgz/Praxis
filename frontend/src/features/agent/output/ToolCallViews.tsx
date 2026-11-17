import { Fragment } from 'react'
import type { AgentEvent, EntryBlock } from '../../../api'
import type { Timing } from '../../../api/timings'
import { CopyIconButton } from './CopyIconButton'
import { FileChanges } from './FileChanges'
import { TerminalOutput } from './TerminalOutput'
import { ToolResultContent } from './ToolResultContent'
import { ActivityItem, activityTone, isOpenByDefault } from './ActivityLedger'
import { formatToolInput, presentTool, toolRecord, type ToolStatus } from './toolPresentation'
import { DurationLabel } from '../../timing/DurationLabel'
import { useToolApproval } from '../ToolApprovals'

export function StreamingToolCallsView({ tools, pendingStatus = 'running' }: { tools: AgentEvent[]; pendingStatus?: ToolStatus }) {
    return <div className="agent-activity output-activity"><div className="acts">
        {groupToolEvents(tools).map((activity) => <ToolCard
            key={activity.key}
            name={activity.call?.data.name || ''}
            input={activity.call?.data.arguments}
            result={activity.result?.data.result}
            isError={activity.result?.data.isError}
            timing={activity.result?.data.timing || activity.started?.data.timing}
            pendingStatus={activity.approval ? 'awaiting_approval' : activity.started ? pendingStatus : 'queued'}
            runId={activity.call?.runId || activity.approval?.runId || activity.result?.runId || ''}
            callId={activity.call?.data.callId || activity.approval?.data.callId || activity.result?.data.callId}
        />)}
    </div></div>
}

export type PairedToolResult = {
    text: string
    isError?: boolean
    timing?: Timing
}

export function ToolCallBlock({ block, events, runId, pairedResult }: { block: EntryBlock; events: AgentEvent[]; runId: string; pairedResult?: PairedToolResult }) {
    const liveResult = events.findLast((event): event is Extract<AgentEvent, { kind: 'tool_result' }> => event.kind === 'tool_result' && event.data.callId === block.callId)
    const started = events.findLast((event): event is Extract<AgentEvent, { kind: 'tool_started' }> => event.kind === 'tool_started' && event.data.callId === block.callId)
    const resultText = liveResult ? liveResult.data.result : pairedResult?.text
    const isError = liveResult ? liveResult.data.isError : pairedResult?.isError
    const timing = liveResult?.data.timing || started?.data.timing || pairedResult?.timing
    return <ToolCard
        name={block.name || ''} input={block.input}
        result={resultText}
        isError={isError}
        timing={timing}
        pendingStatus={started ? 'running' : 'unknown'} runId={runId} callId={block.callId}
    />
}

export function ToolResultBlock({ block, runId, timing }: { block: EntryBlock; runId: string; timing?: Timing }) {
    return <ToolCard name={block.name || ''} result={block.text || ''} isError={block.isError} timing={timing} pendingStatus="unknown" runId={runId} callId={block.callId} />
}

function ToolCard({ name, input, result, isError = false, pendingStatus = 'running', runId, callId, timing }: {
    name: string; input?: unknown; result?: string; isError?: boolean; pendingStatus?: ToolStatus; runId: string; callId?: string; timing?: Timing
}) {
    const approval = useToolApproval(runId, callId)
    const pendingApproval = result === undefined ? approval?.pendingApproval : undefined
    // 没有计时记录时不传元素，活动账本右列会回落到状态词而不是留空。
    const duration = timing ? <DurationLabel record={timing} running={result === undefined && pendingStatus === 'running'} label="" /> : undefined
    // 所有工具都展示后端冻结参数，不能以原始模型请求替换将被批准的操作。
    const toolInput = pendingApproval ? pendingApproval.arguments : input
    const tool = presentTool(name, toolInput, result, isError, pendingStatus)
    if (pendingApproval) tool.status = 'awaiting_approval'
    // 待批准的决策统一在输入区上方的审批闸口完成，卡片内不再重复一组按钮。
    const awaitingApproval = tool.status === 'awaiting_approval'
    if (name === 'bash') return <TerminalOutput tool={tool} duration={duration} />
    if (name === 'apply_patch') return <FileChanges tool={tool} duration={duration} />
    const formattedInput = toolInput !== undefined ? formatToolInput(toolInput) : ''
    const inputRecord = toolInput !== undefined ? toolRecord(toolInput) : {}
    const hasStructuredInput = Object.keys(inputRecord).length > 0
    return <ActivityItem
        tone={activityTone(tool.status)}
        name={tool.name || tool.label || '工具'}
        argument={tool.summary}
        status={tool.status}
        statusLabel={tool.notice.startsWith('搜索无结果') ? '无匹配' : undefined}
        duration={duration}
        open={isOpenByDefault(tool.status)}
        foot={(typeof tool.result.size === 'number' || tool.truncated || tool.notice) ? <>
            {typeof tool.result.size === 'number' && <span>{tool.result.size} 字节</span>}
            {tool.nextOffset !== undefined && <span>已截断 · 下一偏移 {tool.nextOffset}</span>}
            {tool.notice && <span>{tool.notice}</span>}
        </> : undefined}
    >
        {toolInput !== undefined && <section className="output-section output-input">
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
        </section>}
        {!awaitingApproval && <section className="output-section output-result">
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
            {result === undefined ? <p className="output-muted">{tool.status === 'running' ? '正在执行，等待工具返回结果…' : '暂无输出'}</p>
                : tool.content ? <ToolResultContent content={tool.content} toolName={name} />
                : <p className="output-muted">{tool.notice || '暂无输出'}</p>}
        </section>}
    </ActivityItem>
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

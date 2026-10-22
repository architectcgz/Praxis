import type { AgentEvent, EntryBlock } from '../../../api'
import { FileChanges } from './FileChanges'
import { TerminalOutput } from './TerminalOutput'
import { ToolResultContent } from './ToolResultContent'
import { ActivityItem, activityTone, isOpenByDefault } from './ActivityLedger'
import { formatToolInput, presentTool, type ToolStatus } from './toolPresentation'
import { DurationLabel, useOperationTiming } from '../../timing/OperationTimings'
import { useToolApproval } from '../ToolApprovals'

export function StreamingToolCallsView({ tools, pendingStatus = 'running' }: { tools: AgentEvent[]; pendingStatus?: ToolStatus }) {
    return <div className="agent-activity output-activity"><div className="acts">
        {groupToolEvents(tools).map((activity) => <ToolCard
            key={activity.key}
            name={activity.call?.name || activity.approval?.name || activity.result?.name || ''}
            input={activity.call?.input}
            result={activity.result?.result}
            isError={activity.result?.isError}
            pendingStatus={activity.approval ? 'awaiting_approval' : pendingStatus}
            executionId={activity.call?.executionId || activity.approval?.executionId || activity.result?.executionId || ''}
            callId={activity.call?.callId || activity.approval?.callId || activity.result?.callId}
        />)}
    </div></div>
}

export function ToolCallBlock({ block, result, executionId }: { block: EntryBlock; result?: EntryBlock; executionId: string }) {
    return <ToolCard name={block.name || ''} input={block.input} result={result ? result.text || '' : undefined} isError={result?.isError} pendingStatus="unknown" executionId={executionId} callId={block.callId} />
}

export function ToolResultBlock({ block, executionId }: { block: EntryBlock; executionId: string }) {
    return <ToolCard name={block.name || ''} result={block.text || ''} isError={block.isError} pendingStatus="unknown" executionId={executionId} callId={block.callId} />
}

function ToolCard({ name, input, result, isError = false, pendingStatus = 'running', executionId, callId }: {
    name: string; input?: unknown; result?: string; isError?: boolean; pendingStatus?: ToolStatus; executionId: string; callId?: string
}) {
    const record = useOperationTiming(executionId, 'tool', callId)
    const approval = useToolApproval(executionId, callId)
    const pendingApproval = result === undefined ? approval?.pendingApproval : undefined
    // 没有计时记录时不传元素，活动账本右列会回落到状态词而不是留空。
    const duration = record ? <DurationLabel record={record} label="" /> : undefined
    // 所有工具都展示后端冻结参数，不能以原始模型请求替换将被批准的操作。
    const toolInput = pendingApproval ? pendingApproval.arguments : input
    const tool = presentTool(name, toolInput, result, isError, pendingStatus)
    if (result === undefined) {
        if (record?.status === 'failed') tool.status = 'failed'
        else if (record?.status === 'cancelled' || record?.status === 'interrupted') tool.status = 'cancelled'
        else if (record?.status === 'running') tool.status = 'running'
        else if (record?.status === 'completed') tool.status = 'unknown'
        else if (pendingStatus === 'running') tool.status = 'queued'
    }
    if (pendingApproval) tool.status = 'awaiting_approval'
    // 待批准的决策统一在输入区上方的审批闸口完成，卡片内不再重复一组按钮。
    const awaitingApproval = tool.status === 'awaiting_approval'
    if (name === 'bash') return <TerminalOutput tool={tool} duration={duration} />
    if (name === 'apply_patch') return <FileChanges tool={tool} duration={duration} />
    return <ActivityItem
        tone={activityTone(tool.status)}
        name={tool.name || tool.label || '工具'}
        argument={tool.summary}
        status={tool.status}
        statusLabel={tool.notice.startsWith('搜索无结果') ? '无匹配' : undefined}
        duration={duration}
        open={isOpenByDefault(tool.status)}
    >
        {toolInput !== undefined && <section className="output-section output-input">
            <div className="output-section-label">参数</div>
            <pre className="output-text">{formatToolInput(toolInput)}</pre>
        </section>}
        {!awaitingApproval && <section className="output-section output-result">
            <div className="output-section-label">输出</div>
            {result === undefined ? <p className="output-muted">{tool.status === 'running' ? '正在执行，等待工具返回结果…' : '暂无输出'}</p>
                : tool.content ? <ToolResultContent content={tool.content} /> : <p className="output-muted">暂无输出</p>}
            {typeof tool.result.size === 'number' && <div className="output-meta"><span>{tool.result.size} 字节</span></div>}
        </section>}
    </ActivityItem>
}

type ToolActivity = { key: string; call?: AgentEvent; approval?: AgentEvent; result?: AgentEvent }

function groupToolEvents(events: AgentEvent[]): ToolActivity[] {
    const activities: ToolActivity[] = []
    const byCallID = new Map<string, ToolActivity>()
    events.forEach((event, index) => {
        if (event.kind !== 'tool_call' && event.kind !== 'tool_approval_required' && event.kind !== 'tool_result') return
        const key = `${event.executionId}:${event.turnId}:${event.callId || `tool-${index}`}`
        let activity = byCallID.get(key)
        if (!activity) {
            activity = { key }
            byCallID.set(key, activity)
            activities.push(activity)
        }
        if (event.kind === 'tool_call') activity.call = event
        else if (event.kind === 'tool_approval_required') activity.approval = event
        else activity.result = event
    })
    return activities
}

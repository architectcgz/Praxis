import { memo } from 'react'
import { ChevronDown, CircleAlert } from 'lucide-react'
import type { HistoryMessage, ModelRequestActivity } from '../../../api'
import type { RequestActivities } from '../history/entries'
import type { StreamingOutput } from '../types'
import { EntryView } from './EntryView'
import { MessageMarkdown } from './MarkdownViews'
import { MessageTiming } from './MessageTiming'
import type { PairedToolResult } from './ToolCallViews'

function toolCallKey(runId?: string, callId?: string): string {
    return `${runId || ''}:${callId || ''}`
}

type HistoryItem = { message: HistoryMessage } | { request: ModelRequestActivity }
const itemTime = (item: HistoryItem) => ('message' in item ? item.message : item.request).createdAt

/** 配对工具回执，并将失败的请求活动插入相应位置。 */
export function buildEntryHistory(entries: HistoryMessage[], requests: RequestActivities) {
    const assistantTurns = new Set<string>()
    const errorRuns = new Set<string>()
    const toolResultsByCall = new Map<string, PairedToolResult>()
    const visibleCallKeys = new Set<string>()

    for (const item of entries) {
        if (item.role === 'assistant' && item.kind === 'message') {
            assistantTurns.add(JSON.stringify([item.runId, item.turnId]))
            for (const block of item.content) {
                if (block.kind === 'tool_call' && block.callId) visibleCallKeys.add(toolCallKey(item.runId, block.callId))
            }
        }
        if (item.role === 'tool') {
            for (const block of item.content) {
                if (block.kind === 'tool_result' && block.callId) {
                    toolResultsByCall.set(toolCallKey(item.runId, block.callId), {
                        text: block.text || '',
                        isError: block.isError,
                        timing: block.timing || item.timing,
                    })
                }
            }
        }
    }

    const visible: HistoryItem[] = entries
        .filter((item) => item.role !== 'tool' || item.content.some((block) =>
            block.kind === 'tool_result' && (!block.callId || !visibleCallKeys.has(toolCallKey(item.runId, block.callId)))))
        .map((message) => ({ message }))
    for (const request of requests.values()) {
        if (request.result?.status === 'failed' || request.result?.status === 'interrupted') {
            visible.push({ request })
            if (request.result.error) errorRuns.add(request.runId)
        }
    }
    visible.sort((left, right) => Date.parse(itemTime(left)) - Date.parse(itemTime(right)))

    let groupStart = 0
    const rows = visible.map((row, index) => {
        if ('request' in row) return row
        const item = row.message
        const continuation = sameAssistantRun(visible[index - 1], row)
        const hasContinuation = sameAssistantRun(row, visible[index + 1])
        if (!continuation) groupStart = index
        const copyContent = hasContinuation ? '' : visible.slice(groupStart, index + 1)
            .map((entry) => 'message' in entry ? entry.message.content.filter((block) => block.kind === 'text').map((block) => block.text || '').join('') : '')
            .filter(Boolean).join('\n\n')
        const pairedResults = item.role === 'assistant' && item.kind === 'message'
            ? Object.fromEntries(
                item.content.flatMap((block) => {
                    if (block.kind !== 'tool_call' || !block.callId) return []
                    const matched = toolResultsByCall.get(toolCallKey(item.runId, block.callId))
                    return matched ? [[block.callId, matched] as const] : []
                }),
            )
            : undefined
        return { message: item, continuation, hasContinuation, copyContent, pairedResults }
    })
    return { rows, assistantTurns, errorRuns }
}

export const EntryHistory = memo(function EntryHistory({ rows, requests, agentName, streamingOutput, onOpenSubagent }: {
    rows: ReturnType<typeof buildEntryHistory>['rows']
    requests: RequestActivities
    agentName: string
    streamingOutput: StreamingOutput | null
    onOpenSubagent: (id: string) => void
}) {
    return rows.map((row) => {
        if ('request' in row) return <RequestFailure request={row.request} key={`request:${row.request.requestId}`} />
        const { message, continuation, hasContinuation, copyContent, pairedResults } = row
        return <EntryView
            key={message.id}
            agentName={agentName}
            item={message}
            usage={message.requestId ? requests.get(message.requestId)?.result?.usage : undefined}
            events={streamingOutput && streamingOutput.runId === message.runId ? streamingOutput.turns.find((turn) => turn.turnId === message.turnId)?.events : undefined}
            pairedResults={pairedResults}
            continuation={continuation}
            hasContinuation={hasContinuation}
            copyContent={copyContent}
            onOpenSubagent={onOpenSubagent}
        />
    })
})

const requestKindLabels = {
    iteration: '模型迭代',
    risk_review: '风险审核',
    summary: '上下文摘要',
}

function RequestFailure({ request }: { request: ModelRequestActivity }) {
    const result = request.result
    if (!result) return null
    const status = result.status === 'interrupted' ? '已中断' : '失败'
    return <details className="message entry-model-request">
        <summary className="entry-model-request-summary">
            <CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" />
            <span className="entry-model-request-title">{requestKindLabels[request.requestKind]}{status} · 第 {request.retryCount + 1} 次尝试</span>
            <MessageTiming record={request.timing} usage={result.usage} />
            <ChevronDown className="entry-model-request-chevron" size={14} aria-hidden="true" />
        </summary>
        <div className="entry-model-request-body">
            {result.error && <div className="message-error-box" role="alert"><CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" /><span>{result.error}</span></div>}
            {result.partial && <div className="entry-model-request-partial">
                <span className="entry-section-kicker">本次请求的部分输出</span>
                <MessageMarkdown content={result.partial} />
            </div>}
        </div>
    </details>
}

function sameAssistantRun(left?: HistoryItem, right?: HistoryItem): boolean {
    if (!left || !right || !('message' in left) || !('message' in right)) return false
    const a = left.message
    const b = right.message
    return a.kind === 'message' && b.kind === 'message' && a.role === 'assistant' && b.role === 'assistant' && a.runId === b.runId
}

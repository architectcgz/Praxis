import { memo } from 'react'
import type { Entry } from '../../../api'
import type { StreamingOutput } from '../types'
import { EntryView } from './EntryView'
import type { PairedToolResult } from './ToolCallViews'

function toolCallKey(runId?: string, callId?: string): string {
    return `${runId || ''}:${callId || ''}`
}

/** 历史派生数据只随持久化条目更新，配对工具回执并保留相邻助手消息连续性。 */
export function buildEntryHistory(entries: Entry[]) {
    const assistantTurns = new Set<string>()
    const errorRuns = new Set<Entry['runId']>()
    const toolResultsByCall = new Map<string, PairedToolResult>()
    const visibleCallKeys = new Set<string>()

    for (const item of entries) {
        if (item.role === 'assistant' && item.kind === 'message') {
            assistantTurns.add(JSON.stringify([item.runId, item.turnId]))
            for (const block of item.content) {
                if (block.kind === 'tool_call' && block.callId) {
                    visibleCallKeys.add(toolCallKey(item.runId, block.callId))
                }
            }
        }
        if (item.error) errorRuns.add(item.runId)
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

    // 所有回执均已配对到可见 assistant tool_call 的工具条目不再重复渲染为孤立卡片。
    const visibleEntries = entries.filter((item) => {
        if (item.kind === 'run') return false
        if (item.role !== 'tool') return true
        const results = item.content.filter((block) => block.kind === 'tool_result')
        if (results.length === 0) return false
        return results.some((block) => !block.callId || !visibleCallKeys.has(toolCallKey(item.runId, block.callId)))
    })

    let groupStart = 0
    const rows = visibleEntries.map((item, index) => {
        const continuation = sameAssistantRun(visibleEntries[index - 1], item)
        const hasContinuation = sameAssistantRun(item, visibleEntries[index + 1])
        if (!continuation) groupStart = index
        const copyContent = hasContinuation ? '' : visibleEntries.slice(groupStart, index + 1)
            .map((entry) => entry.content.filter((block) => block.kind === 'text').map((block) => block.text || '').join(''))
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
        return { item, continuation, hasContinuation, copyContent, pairedResults }
    })
    return { rows, assistantTurns, errorRuns }
}

/** 保留每条消息的稳定 key；只有已落盘回合的工具实时事件需要穿透历史边界。 */
export const EntryHistory = memo(function EntryHistory({ rows, agentName, streamingOutput }: {
    rows: ReturnType<typeof buildEntryHistory>['rows']
    agentName: string
    streamingOutput: StreamingOutput | null
}) {
    return rows.map(({ item, continuation, hasContinuation, copyContent, pairedResults }) => <EntryView
        key={item.id}
        agentName={agentName}
        item={item}
        events={streamingOutput && streamingOutput.runId === item.runId ? streamingOutput.turns.find((turn) => turn.turnId === item.turnId)?.events : undefined}
        pairedResults={pairedResults}
        continuation={continuation}
        hasContinuation={hasContinuation}
        copyContent={copyContent}
    />)
})

/** 仅衔接同一次执行中相邻的助手消息；用户消息、错误和不同执行都保留独立边界。 */
function sameAssistantRun(left?: Entry, right?: Entry): boolean {
    return left?.kind === 'message' && right?.kind === 'message' &&
        left.role === 'assistant' && right.role === 'assistant' &&
        !left.error && !right.error && !!left.runId && left.runId === right.runId
}

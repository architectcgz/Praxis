import { memo } from 'react'
import type { Entry } from '../../../api'
import type { StreamingOutput } from '../types'
import { EntryView } from './EntryView'

/** 历史派生数据只随持久化条目更新，不随文本增量重新分组或拼接复制内容。 */
export function buildEntryHistory(entries: Entry[]) {
    const assistantTurns = new Set<string>()
    const errorRuns = new Set<Entry['runId']>()
    let groupStart = 0
    const rows = entries.map((item, index) => {
        if (item.role === 'assistant') assistantTurns.add(JSON.stringify([item.runId, item.turnId]))
        if (item.error) errorRuns.add(item.runId)
        const continuation = sameAssistantTask(entries[index - 1], item)
        const hasContinuation = sameAssistantTask(item, entries[index + 1])
        if (!continuation) groupStart = index
        const copyContent = hasContinuation ? '' : entries.slice(groupStart, index + 1)
            .map((entry) => entry.content.filter((block) => block.kind === 'text').map((block) => block.text || '').join(''))
            .filter(Boolean).join('\n\n')
        return { item, continuation, hasContinuation, copyContent }
    })
    return { rows, assistantTurns, errorRuns }
}

/** 保留每条消息的稳定 key；只有已落盘回合的工具实时事件需要穿透历史边界。 */
export const EntryHistory = memo(function EntryHistory({ rows, agentName, streamingOutput }: {
    rows: ReturnType<typeof buildEntryHistory>['rows']
    agentName: string
    streamingOutput: StreamingOutput | null
}) {
    return rows.map(({ item, continuation, hasContinuation, copyContent }) => <EntryView
        key={item.id}
        agentName={agentName}
        item={item}
        events={streamingOutput && streamingOutput.runId === item.runId ? streamingOutput.turns.find((turn) => turn.turnId === item.turnId)?.events : undefined}
        continuation={continuation}
        hasContinuation={hasContinuation}
        copyContent={copyContent}
    />)
})

/** 仅衔接同一次执行中相邻的助手消息；用户消息、错误和不同任务都保留独立边界。 */
function sameAssistantTask(left?: Entry, right?: Entry): boolean {
    return left?.role === 'assistant' && right?.role === 'assistant' &&
        !left.error && !right.error && !!left.runId && left.runId === right.runId
}

import type { Timing } from '../../../api/timings'
import { DurationLabel } from '../../timing/DurationLabel'
import { formatDuration } from '../../timing/timing'
import { TokenUsageBreakdown, useSessionModelUsage } from '../SessionTokenUsage'
import { formatModelUsage, summarizeModelUsage } from '../modelUsage'

/** 消费当前响应自身的耗时，已上报用量按请求身份关联。 */
export function MessageTiming({ runId, turnId, record, running = false }: {
    runId: string
    turnId?: string
    record?: Timing
    running?: boolean
}) {
    const { records, error } = useSessionModelUsage()
    const request = records.find((item) => item.runId === runId && item.turnId === turnId)
    const usage = request ? formatModelUsage(summarizeModelUsage([request])) : undefined
    const firstResponse = record?.firstResponseMs === undefined ? [] : [`首响应：${formatDuration(record.firstResponseMs)}`]
    const title = usage
        ? ['本次模型请求', usage.details, ...firstResponse].join('\n')
        : error ? `Token 用量加载失败：${error}` : ''
    if (!record && !usage && !error) return null
    return <span className="message-timing">
        <DurationLabel record={record} running={running} label="" compact />
        {(usage || error) && <span className="operation-token-usage" title={title} aria-label={title}>
            {usage ? <TokenUsageBreakdown labels={usage.labels} /> : '用量不可用'}
        </span>}
    </span>
}

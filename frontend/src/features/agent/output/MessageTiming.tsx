import type { ModelUsage } from '../../../api'
import type { Timing } from '../../../api/timings'
import { DurationLabel } from '../../timing/DurationLabel'
import { formatDuration } from '../../timing/timing'
import { TokenUsageBreakdown } from '../SessionTokenUsage'
import { formatRequestUsage } from '../modelUsage'

export function MessageTiming({ record, usage, running = false }: {
    record?: Timing
    usage?: ModelUsage
    running?: boolean
}) {
    if (!record && !usage) return null
    const counts = usage && formatRequestUsage(usage)
    const firstResponse = record?.firstResponseMs === undefined ? [] : [`首响应：${formatDuration(record.firstResponseMs)}`]
    const title = counts ? ['本次模型请求', counts.details, ...firstResponse].join('\n') : ''
    return <span className="message-timing">
        <DurationLabel record={record} running={running} label="" compact />
        {counts && <span className="operation-token-usage" title={title} aria-label={title}>
            <TokenUsageBreakdown labels={counts.labels} />
        </span>}
    </span>
}

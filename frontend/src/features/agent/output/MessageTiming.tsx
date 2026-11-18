import type { ModelUsage, Timing } from '../../../api'
import { DurationLabel } from '../../timing/DurationLabel'
import { formatDuration } from '../../timing/timing'
import { formatRequestUsage } from '../modelUsage'

export function MessageTiming({ record, usage, running = false }: {
    record?: Timing | null
    usage?: ModelUsage
    running?: boolean
}) {
    if (!record && !usage) return null
    const counts = usage && formatRequestUsage(usage)
    const firstResponse = record?.firstResponseMs == null ? [] : [`首响应：${formatDuration(record.firstResponseMs)}`]
    const title = counts ? ['本次模型请求', counts.details, ...firstResponse].join('\n') : ''
    return <span className="message-timing">
        <DurationLabel record={record} running={running} label="" compact />
        {counts && <span className="operation-token-usage" title={title} aria-label={title}>
            {counts.labels.map((label) => <span key={label}>{label} </span>)}
        </span>}
    </span>
}

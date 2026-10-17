import { DurationLabel, useOperationTiming } from '../../timing/OperationTimings'
import { formatDuration } from '../../timing/timing'
import { TokenUsageBreakdown, useSessionModelUsage } from '../SessionTokenUsage'
import { formatModelUsage, summarizeModelUsage } from '../modelUsage'

const statusLabels = { running: '进行中', completed: '已完成', failed: '失败', cancelled: '已取消', interrupted: '已中断' }

/** 展示每轮请求的耗时与分项用量；精确计数与首响应保留在悬浮提示中。 */
export function MessageTiming({ taskId, referenceId }: { taskId: string; referenceId: string }) {
    const record = useOperationTiming(taskId, 'provider', referenceId)
    const { records, loading, error } = useSessionModelUsage()
    if (!record) return null
    const request = records.find((item) => item.sessionId === record.sessionId && item.agentId === record.agentId &&
        item.taskId === taskId && referenceId === `assistant:${item.turnId}`)
    const usage = request ? formatModelUsage(summarizeModelUsage([request])) : undefined
    const tokens = loading ? '… tokens' : error ? '用量不可用' : '-- tokens'
    const firstResponse = record.firstResponseMs === undefined ? [] : [`首响应：${formatDuration(record.firstResponseMs)}`]
    const title = usage
        ? ['本次模型请求', usage.details, ...firstResponse].join('\n')
        : loading ? '正在加载本次模型请求的 token 用量' : error ? `Token 用量加载失败：${error}` : '本次模型请求尚未上报 token 用量'
    // 状态词只在非正常结束时出现；正常完成不占脚注的位置。
    return <span className="message-timing">
        {record.status !== 'completed' && <span className="message-timing-status" data-tone={record.status === 'failed' ? 'bad' : 'warn'}>{statusLabels[record.status]}</span>}
        <DurationLabel record={record} label="" compact />
        <span className="operation-token-usage" title={title} aria-label={title}>
            {usage ? <TokenUsageBreakdown labels={usage.labels} /> : tokens}
        </span>
    </span>
}

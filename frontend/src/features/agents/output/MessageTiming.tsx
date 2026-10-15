import { DurationLabel, useOperationTiming } from '../../timing/OperationTimings'
import { TokenUsageBreakdown, useSessionModelUsage } from '../SessionTokenUsage'
import { formatModelUsage, summarizeModelUsage } from '../modelUsage'

const statusLabels = { running: '进行中', completed: '已完成', failed: '失败', cancelled: '已取消', interrupted: '已中断' }

/** 按消息引用展示每轮 Provider 状态、耗时与用量；缺失输出不补零，缓存不重复计入。 */
export function MessageTiming({ taskId, referenceId }: { taskId: string; referenceId: string }) {
    const record = useOperationTiming(taskId, 'provider', referenceId)
    const { records, loading, error } = useSessionModelUsage()
    if (!record) return null
    const request = records.find((item) => item.sessionId === record.sessionId && item.agentId === record.agentId &&
        item.taskId === taskId && referenceId === `assistant:${item.turnId}`)
    const usage = request ? formatModelUsage(summarizeModelUsage([request])) : undefined
    const tokens = loading ? '... tokens' : error ? '用量不可用' : '-- tokens'
    const title = usage ? `本次模型请求\n${usage.details}`
        : loading ? '正在加载本次模型请求的 token 用量' : error ? `Token 用量加载失败：${error}` : '本次模型请求尚未上报 token 用量'
    return <span className="message-timing">
        <span>模型请求</span>
        <span>{statusLabels[record.status]}</span>
        <DurationLabel record={record} />
        <span className="operation-duration operation-token-usage" title={title} aria-label={title}>
            {usage ? <TokenUsageBreakdown labels={usage.labels} /> : tokens}
        </span>
    </span>
}

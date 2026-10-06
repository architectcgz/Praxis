import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { subscribeAgentEvents } from '../../api/agents'
import { listAgentTimings, type OperationTiming } from '../../api/timings'
import { readableError } from '../../shared/errors'
import { TokenUsageBreakdown, useSessionModelUsage } from '../agents/SessionTokenUsage'
import { formatModelUsage, summarizeModelUsage } from '../agents/modelUsage'
import { findTiming, formatDuration, mergeTimings } from './timing'
import './styles.css'

const TimingContext = createContext<OperationTiming[]>([])

/** 独立加载计时与订阅变更；查询失败不阻断对话，切换会话后丢弃过期响应。 */
export function OperationTimingsProvider({ agentIds, children }: { agentIds: string[]; children: ReactNode }) {
    const [records, setRecords] = useState<OperationTiming[]>([])
    const [errors, setErrors] = useState<Record<string, string>>({})
    const key = JSON.stringify([...agentIds].sort())
    useEffect(() => {
        let active = true
        const ids = new Set<string>(JSON.parse(key))
        setRecords((current) => current.filter((record) => ids.has(record.agentId)))
        setErrors({})
        const load = async (agentId: string) => {
            try {
                const loaded = await listAgentTimings(agentId)
                if (active) {
                    setRecords((current) => mergeTimings(current, loaded))
                    setErrors((current) => { const next = { ...current }; delete next[agentId]; return next })
                }
            } catch (error) {
                if (active) setErrors((current) => ({ ...current, [agentId]: readableError(error) }))
            }
        }
        // 先订阅再查询，使用版本合并消除查询期间的事件竞争。
        const unsubscribe = subscribeAgentEvents((event) => {
            if (!ids.has(event.agentId)) return
            if (event.kind === 'operation_timing' && event.timing) setRecords((current) => mergeTimings(current, [event.timing!]))
            if (event.kind === 'turn_ended' || event.kind === 'request_canceled') void load(event.agentId)
        })
        for (const id of ids) void load(id)
        return () => { active = false; unsubscribe() }
    }, [key])
    return <TimingContext value={records}>
        {Object.keys(errors).length > 0 && <p className="operation-timing-error" role="status">耗时记录加载失败：{Object.values(errors).join('；')}</p>}
        {children}
    </TimingContext>
}

/** 查找关联操作的独立计时记录；没有记录时返回 undefined。 */
export function useOperationTiming(turnId: string, kind: OperationTiming['kind'], referenceId?: string) {
    return findTiming(useContext(TimingContext), turnId, kind, referenceId)
}

/** 运行中显示估算值，结束后只显示后端单调时钟测量值；异常退出不持续计时。 */
export function DurationLabel({ record, label = '耗时' }: { record?: OperationTiming; label?: string }) {
    const [now, setNow] = useState(Date.now)
    const running = record?.status === 'running'
    useEffect(() => {
        if (!running) return
        setNow(Date.now())
        const timer = window.setInterval(() => setNow(Date.now()), 1000)
        return () => window.clearInterval(timer)
    }, [record?.id, running])
    if (!record) return null
    const elapsed = running ? Math.max(0, now - Date.parse(record.startedAt)) : record.durationMs
    const text = `${label} ${elapsed === undefined ? '未知（中断）' : `${running ? '约 ' : ''}${formatDuration(elapsed)}`}`
    const firstResponse = record.firstResponseMs === undefined ? '' : ` · 首响应 ${formatDuration(record.firstResponseMs)}`
    return <span className="operation-duration" title={`${text}${firstResponse}${running ? '；运行中为估算值，结束后使用后端实测耗时' : ''}`}>
        {text}{firstResponse}
    </span>
}

/** 将计时标签关联到消息或工具卡片，不把计时信息拼进业务内容。 */
export function OperationDuration({ turnId, kind, referenceId, label }: { turnId: string; kind: OperationTiming['kind']; referenceId?: string; label?: string }) {
    return <DurationLabel record={useOperationTiming(turnId, kind, referenceId)} label={label} />
}

const statusLabels: Record<OperationTiming['status'], string> = {
    running: '进行中', completed: '已完成', failed: '失败', cancelled: '已取消', interrupted: '已中断',
}
/** 按消息引用展示每轮 Provider 状态、耗时与用量；缺失输出不补零，缓存不重复计入。 */
export function MessageTiming({ turnId, referenceId }: { turnId: string; referenceId: string }) {
    const record = useOperationTiming(turnId, 'provider', referenceId)
    const { records, loading, error } = useSessionModelUsage()
    if (!record) return null
    const request = records.find((item) => item.sessionId === record.sessionId && item.agentId === record.agentId &&
        item.turnId === turnId && referenceId === `assistant:${item.turnId}:${item.step}`)
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

/** 在当前 Agent 名称旁显示执行中的总计时；执行结束或计时缺失时不展示。 */
export function AgentRuntime({ turnId }: { turnId: string }) {
    const record = useOperationTiming(turnId, 'agent')
    return record?.status === 'running' ? <DurationLabel record={record} label="运行中" /> : null
}

import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { subscribeAgentEvents } from '../../api/agents'
import { listAgentTimings, type OperationTiming } from '../../api/timings'
import { readableError } from '../../shared/errors'
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
            if (event.kind === 'task_ended' || event.kind === 'request_canceled') void load(event.agentId)
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
export function useOperationTiming(taskId: string, kind: OperationTiming['kind'], referenceId?: string) {
    return findTiming(useContext(TimingContext), taskId, kind, referenceId)
}

/** 运行中显示估算值，结束后只显示后端单调时钟测量值；异常退出不持续计时。
    compact 供脚注这类单值场景使用：可见文本只留耗时，首响应留在 title 里。 */
export function DurationLabel({ record, label = '耗时', compact = false }: { record?: OperationTiming; label?: string; compact?: boolean }) {
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
    const value = elapsed === undefined ? '未知（中断）' : `${running ? '约 ' : ''}${formatDuration(elapsed)}`
    // 传空 label 表示只显示数字，供活动账本这类需要紧凑单值的场景使用。
    const text = label ? `${label} ${value}` : value
    const firstResponse = record.firstResponseMs === undefined ? '' : ` · 首响应 ${formatDuration(record.firstResponseMs)}`
    return <span className="operation-duration" title={`${text}${firstResponse}${running ? '；运行中为估算值，结束后使用后端实测耗时' : ''}`}>
        {text}{compact ? '' : firstResponse}
    </span>
}

/** 将计时标签关联到消息或工具卡片，不把计时信息拼进业务内容。 */
export function OperationDuration({ taskId, kind, referenceId, label }: { taskId: string; kind: OperationTiming['kind']; referenceId?: string; label?: string }) {
    return <DurationLabel record={useOperationTiming(taskId, kind, referenceId)} label={label} />
}

/** 在当前 Agent 名称旁显示执行中的总计时；执行结束或计时缺失时不展示。 */
export function AgentRuntime({ taskId }: { taskId: string }) {
    const record = useOperationTiming(taskId, 'agent')
    return record?.status === 'running' ? <DurationLabel record={record} label="运行中" /> : null
}

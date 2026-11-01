import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import type { OperationTiming } from '../../api/timings'
import { findTiming, formatDuration } from './timing'
import './styles.css'

const TimingContext = createContext<OperationTiming[]>([])

/** 展示层只消费会话快照与实时事件合并后的计时，不发起独立查询。 */
export function OperationTimingsProvider({ records, children }: { records: OperationTiming[]; children: ReactNode }) {
    return <TimingContext value={records}>{children}</TimingContext>
}

/** 查找关联操作的独立计时记录；没有记录时返回 undefined。 */
export function useOperationTiming(executionId: string, kind: OperationTiming['kind'], referenceId?: string) {
    return findTiming(useContext(TimingContext), executionId, kind, referenceId)
}

/** 运行中显示估算值，结束后只显示后端单调时钟测量值；异常退出不持续计时。
    compact 供脚注这类单值场景使用：可见文本只留耗时，首响应留在 title 里。 */
export function DurationLabel({ record, label = '耗时', compact = false }: { record?: OperationTiming; label?: string; compact?: boolean }) {
    const [now, setNow] = useState(Date.now)
    const running = record?.status === 'running'
    useEffect(() => {
        if (!running) return
        setNow(Date.now())
        // 刷新频率与 0.1 秒的展示精度一致，不依赖模型输出触发重绘。
        const timer = window.setInterval(() => setNow(Date.now()), 100)
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
export function OperationDuration({ executionId, kind, referenceId, label }: { executionId: string; kind: OperationTiming['kind']; referenceId?: string; label?: string }) {
    return <DurationLabel record={useOperationTiming(executionId, kind, referenceId)} label={label} />
}

/** 在当前 Agent 名称旁显示执行中的总计时；执行结束或计时缺失时不展示。 */
export function AgentDuration({ executionId }: { executionId: string }) {
    const record = useOperationTiming(executionId, 'agent')
    return record?.status === 'running' ? <DurationLabel record={record} label="运行中" /> : null
}

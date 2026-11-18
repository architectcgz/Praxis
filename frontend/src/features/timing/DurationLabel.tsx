import { useEffect, useState } from 'react'
import type { Timing } from '../../api'
import { formatDuration } from './timing'
import './styles.css'

/** 耗时直接来自所属条目；是否正在执行由调用方的业务状态决定。 */
export function DurationLabel({ record, running = false, label = '耗时', compact = false }: {
    record?: Timing | null
    running?: boolean
    label?: string
    compact?: boolean
}) {
    const elapsed = useElapsed(record?.startedAt, running, record?.durationMs ?? undefined)
    if (!record) return null
    const value = elapsed === undefined ? '未知（中断）' : `${running ? '约 ' : ''}${formatDuration(elapsed)}`
    const text = label ? `${label} ${value}` : value
    const firstResponse = record.firstResponseMs == null ? '' : ` · 首响应 ${formatDuration(record.firstResponseMs)}`
    return <span className="operation-duration" title={`${text}${firstResponse}${running ? '；运行中为估算值，结束后使用后端实测耗时' : ''}`}>
        {text}{compact ? '' : firstResponse}
    </span>
}

/** Run 总耗时直接读取其生命周期，不构造独立计时记录。 */
export function RunDuration({ startedAt, endedAt, label = '耗时' }: { startedAt?: string; endedAt?: string; label?: string }) {
    const running = Boolean(startedAt && !endedAt)
    const duration = startedAt && endedAt ? Math.max(0, Date.parse(endedAt) - Date.parse(startedAt)) : undefined
    const elapsed = useElapsed(startedAt, running, duration)
    if (elapsed === undefined) return null
    const text = `${label} ${running ? '约 ' : ''}${formatDuration(elapsed)}`
    return <span className="operation-duration" title={text}>{text}</span>
}

function useElapsed(startedAt: string | undefined, running: boolean, duration: number | undefined) {
    const [now, setNow] = useState(Date.now)
    useEffect(() => {
        if (!running) return
        setNow(Date.now())
        // 刷新频率与 0.1 秒的展示精度一致，不依赖文本增量触发重绘。
        const timer = window.setInterval(() => setNow(Date.now()), 100)
        return () => window.clearInterval(timer)
    }, [startedAt, running])
    return running && startedAt ? Math.max(0, now - Date.parse(startedAt)) : duration
}

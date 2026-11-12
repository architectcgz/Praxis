import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { listSessionUsage, subscribeAgentEvents, type Entry, type ModelUsageRecord } from '../../api/agents'
import { getSessionUsageSummary, type SessionUsageSummary } from '../../api/sessions'
import { readableError } from '../../shared/errors'
import { formatModelUsage, mergeModelUsageRecords } from './modelUsage'

type ModelUsageState = { records: ModelUsageRecord[]; summary: SessionUsageSummary | null; loading: boolean; error: string; recordsError: string }
const ModelUsageContext = createContext<ModelUsageState>({ records: [], summary: null, loading: false, error: '', recordsError: '' })

/** 会话累计独立查询，Turn 明细只保留当前加载的 Entry 范围。 */
export function ModelUsageProvider({ sessionId, revision, entries, children }: { sessionId: string; revision: number; entries: Entry[]; children: ReactNode }) {
    const [records, setRecords] = useState<ModelUsageRecord[]>([])
    const [summary, setSummary] = useState<SessionUsageSummary | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    const [recordsError, setRecordsError] = useState('')
    useEffect(() => {
        let active = true
        let request = 0
        setSummary(null)
        setLoading(true)
        setError('')
        const load = async () => {
            const id = ++request
            try {
                const loaded = await getSessionUsageSummary(sessionId)
                if (active && id === request) {
                    setSummary(loaded)
                    setError('')
                }
            } catch (err) {
                if (active && id === request) setError(readableError(err))
            } finally {
                if (active && id === request) setLoading(false)
            }
        }
        const unsubscribe = subscribeAgentEvents((event) => {
            if (!active) return
            if (event.kind === 'model_request_finished' && event.sessionId === sessionId) {
                // 后端已接纳请求结果，重新查询同一业务快照的 Run 汇总。
                void load()
            }
            // 回合结束时重新查询，可恢复断线或未及时收到的用量事件。
            if ((event.kind === 'run_ended' || event.kind === 'request_canceled') && event.sessionId === sessionId) void load()
        })
        void load()
        return () => { active = false; unsubscribe() }
    }, [sessionId, revision])
    const turnKey = useMemo(() => JSON.stringify([...new Set(entries.map((entry) => entry.turnId).filter(Boolean))].sort()), [entries])
    useEffect(() => {
        let active = true
        const turnIds = JSON.parse(turnKey) as string[]
        const wanted = new Set(turnIds)
        setRecordsError('')
        setRecords((current) => current.filter((record) => wanted.has(record.turnId)))
        const load = async () => {
            try {
                const incoming: ModelUsageRecord[] = []
                for (let start = 0; start < turnIds.length; start += 2000) incoming.push(...await listSessionUsage(sessionId, turnIds.slice(start, start + 2000)))
                if (active) setRecords((current) => mergeModelUsageRecords(current, incoming))
            } catch (reason) { if (active) setRecordsError(readableError(reason)) }
        }
        const unsubscribe = subscribeAgentEvents((event) => {
            if (event.sessionId === sessionId && event.kind === 'model_request_finished' && event.turnId && wanted.has(event.turnId) && event.data.usage) {
                const record = { sessionId, runId: event.runId, turnId: event.turnId, usage: event.data.usage }
                setRecords((current) => mergeModelUsageRecords(current, [record]))
            }
        })
        void load()
        return () => { active = false; unsubscribe() }
    }, [sessionId, turnKey])
    const value = useMemo(() => ({ records, summary, loading, error, recordsError }), [records, summary, loading, error, recordsError])
    return <ModelUsageContext value={value}>{children}</ModelUsageContext>
}

/** 读取当前会话的用量快照；没有 Provider 时视为尚无上报记录。 */
export function useSessionModelUsage() {
    return useContext(ModelUsageContext)
}

/** 分项计数各自保留完整文本，窄窗口只在分项之间换行。 */
export function TokenUsageBreakdown({ labels }: { labels: string[] }) {
    return labels.map((label) => <span key={label}>{label} </span>)
}

const percent = new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 })

/** 同一后端快照展示会话已上报的用量与缓存率；未上报项隐藏，零值保留。 */
export function SessionTokenUsage() {
    const { summary, loading, error } = useSessionModelUsage()
    const usage = summary ? formatModelUsage({
        inputTokens: summary.inputTokens,
        outputTokens: summary.outputTokens,
        totalTokens: summary.inputTokens + summary.outputTokens,
        cacheReadInputTokens: summary.cacheReadInputTokens,
        cacheCreationInputTokens: summary.cacheCreationInputTokens,
        missingInput: summary.reportedRequests < summary.totalRequests,
        hasOutput: summary.outputReports > 0,
        missingOutput: summary.outputReports < summary.totalRequests,
        hasCacheRead: summary.cacheReadReports > 0,
        hasCacheCreation: summary.cacheCreationReports > 0,
        missingCacheRead: summary.cacheReadReports < summary.totalRequests,
        missingCacheCreation: summary.cacheCreationReports < summary.totalRequests,
    }) : undefined
    const ratio = summary?.cacheReadRatio
    const cacheLabels = ratio == null ? [] : [`缓存率 ${percent.format(ratio)}`]
    const cacheDetails = ratio == null ? [] : [`缓存率：${percent.format(ratio)}`]
    if (!summary?.totalRequests && !error) return null
    const title = error ? `Token 用量加载失败：${error}`
        : ['会话累计（已上报请求，包含所有 Agent）', usage?.details, ...cacheDetails].filter(Boolean).join('\n')
    return <span className="composer-token-usage" title={title} aria-label={title} aria-busy={loading}>
        {error ? '用量不可用' : !summary?.reportedRequests ? '用量未上报' : <TokenUsageBreakdown labels={[...(usage?.labels ?? []), ...cacheLabels]} />}
    </span>
}

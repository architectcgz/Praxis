import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { subscribeAgentEvents, type ModelUsageRecord } from '../../api/agents'
import { getSessionUsageSummary, type SessionUsageSummary } from '../../api/sessions'
import { readableError } from '../../shared/errors'
import { formatModelUsage, mergeModelUsageRecords, summarizeModelUsage } from './modelUsage'

type ModelUsageState = { records: ModelUsageRecord[]; summary: SessionUsageSummary | null; loading: boolean; error: string }
const ModelUsageContext = createContext<ModelUsageState>({ records: [], summary: null, loading: false, error: '' })

/** 为会话总量和逐次请求共享用量；先订阅再查询，切换会话时丢弃旧响应。 */
export function ModelUsageProvider({ sessionId, children }: { sessionId: string; children: ReactNode }) {
    const [records, setRecords] = useState<ModelUsageRecord[]>([])
    const [summary, setSummary] = useState<SessionUsageSummary | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    useEffect(() => {
        let active = true
        let request = 0
        setRecords([])
        setSummary(null)
        setLoading(true)
        setError('')
        const load = async () => {
            const id = ++request
            try {
                const loaded = await getSessionUsageSummary(sessionId)
                if (active && id === request) {
                    setRecords((current) => mergeModelUsageRecords(current, loaded.records))
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
            if (event.kind === 'model_usage' && event.sessionId === sessionId && event.usage && event.step) {
                const record = { sessionId, agentId: event.agentId, turnId: event.turnId, step: event.step, usage: event.usage }
                setRecords((current) => mergeModelUsageRecords(current, [record]))
                // 后端在事件发布前尝试保存用量；保存失败的实时明细不计入持久化汇总。
                void load()
            }
            // 回合结束时重新查询，可恢复断线或未及时收到的用量事件。
            if ((event.kind === 'turn_ended' || event.kind === 'request_canceled') && event.sessionId === sessionId) void load()
        })
        void load()
        return () => { active = false; unsubscribe() }
    }, [sessionId])
    return <ModelUsageContext value={{ records, summary, loading, error }}>{children}</ModelUsageContext>
}

/** 读取当前会话的用量快照；没有 Provider 时视为尚无上报记录。 */
export function useSessionModelUsage() {
    return useContext(ModelUsageContext)
}

/** 分项计数各自保留完整文本，窄窗口由所属用量容器换行。 */
export function TokenUsageBreakdown({ labels }: { labels: string[] }) {
    return labels.map((label, index) => <span key={index}>{label}{index < labels.length - 1 ? ' ' : ''}</span>)
}

const percent = new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 })

/** 同一后端快照展示会话总量与缓存率；实时明细仍独立合并，未知计数不补零。 */
export function SessionTokenUsage() {
    const { summary, loading, error } = useSessionModelUsage()
    const records = summary?.records ?? []
    const usage = formatModelUsage(summarizeModelUsage(records))
    const ratio = summary?.cacheReadRatio
    const cacheLabel = `缓存率 ${ratio == null ? '--' : percent.format(ratio)}`
    const cacheDetails = ratio != null ? `缓存率：${percent.format(ratio)}`
        : summary?.cacheReadComplete === false ? '缓存率：未知（部分缓存读取计数未上报）' : '缓存率：未知（暂无输入 token）'
    const text = loading ? '... tokens' : error ? '用量不可用' : records.length === 0 ? '-- tokens' : ''
    const title = loading ? '正在加载会话 token 用量' : error ? `Token 用量加载失败：${error}` : records.length === 0 ? '暂无模型上报的 token 用量'
        : `会话累计（仅已保存的请求，包含所有 Agent）\n${usage.details}\n${cacheDetails}`
    return <span className="composer-token-usage" title={title} aria-label={title} aria-busy={loading}>
        {text || <TokenUsageBreakdown labels={[...usage.labels, cacheLabel]} />}
    </span>
}

import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { listSessionUsage, subscribeAgentEvents, type ModelUsageRecord } from '../../api/agents'
import { readableError } from '../../shared/errors'
import { mergeModelUsageRecords, summarizeModelUsage } from './modelUsage'

const compactCount = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })
const exactCount = new Intl.NumberFormat('zh-CN')

type ModelUsageState = { records: ModelUsageRecord[]; loading: boolean; error: string }
const ModelUsageContext = createContext<ModelUsageState>({ records: [], loading: false, error: '' })

/** 为会话总量和逐次请求共享用量；先订阅再查询，切换会话时丢弃旧响应。 */
export function ModelUsageProvider({ sessionId, children }: { sessionId: string; children: ReactNode }) {
    const [records, setRecords] = useState<ModelUsageRecord[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    useEffect(() => {
        let active = true
        let request = 0
        setRecords([])
        setLoading(true)
        setError('')
        const load = async () => {
            const id = ++request
            try {
                const loaded = await listSessionUsage(sessionId)
                if (active && id === request) {
                    setRecords((current) => mergeModelUsageRecords(current, loaded))
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
            }
            // 结算时重新查询，可恢复断线或未及时收到的用量事件。
            if (event.kind === 'settled' && event.sessionId === sessionId) void load()
        })
        void load()
        return () => { active = false; unsubscribe() }
    }, [sessionId])
    return <ModelUsageContext value={{ records, loading, error }}>{children}</ModelUsageContext>
}

/** 读取当前会话的用量快照；没有 Provider 时视为尚无上报记录。 */
export function useSessionModelUsage() {
    return useContext(ModelUsageContext)
}

/** 展示会话所有 Agent 的已上报 token；缓存不重复计入输入和输出之和。 */
export function SessionTokenUsage() {
    const { records, loading, error } = useSessionModelUsage()
    const usage = summarizeModelUsage(records)
    const text = loading ? '... tokens' : error ? '用量不可用' : records.length === 0 ? '-- tokens'
        : `${compactCount.format(usage.totalTokens)}${usage.missingOutput ? '+' : ''} tokens`
    const title = loading ? '正在加载会话 token 用量' : error ? `Token 用量加载失败：${error}` : records.length === 0 ? '暂无模型上报的 token 用量'
        : [
            '会话累计（仅已上报的请求，包含所有 Agent）',
            `输入：${exactCount.format(usage.inputTokens)} tokens`,
            `输出：${exactCount.format(usage.outputTokens)} tokens${usage.missingOutput ? '（部分请求未上报）' : ''}`,
            `缓存读取：${usage.hasCacheRead ? exactCount.format(usage.cacheReadInputTokens) + ' tokens' + (usage.missingCacheRead ? '（部分请求未上报）' : '') : '未上报'}`,
            `缓存写入：${usage.hasCacheCreation ? exactCount.format(usage.cacheCreationInputTokens) + ' tokens' + (usage.missingCacheCreation ? '（部分请求未上报）' : '') : '未上报'}`,
        ].join('\n')
    return <span className="composer-token-usage" title={title} aria-label={title} aria-busy={loading}>{text}</span>
}

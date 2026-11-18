import type { SessionUsageSummary } from '../../api'
import { formatModelUsage } from './modelUsage'

/** 分项计数各自保留完整文本，窄窗口只在分项之间换行。 */
export function TokenUsageBreakdown({ labels }: { labels: string[] }) {
    return labels.map((label) => <span key={label}>{label} </span>)
}

const percent = new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 })

/** 展示历史快照与实时请求结果合计的会话用量。 */
export function SessionTokenUsage({ summary }: { summary: SessionUsageSummary | null }) {
    if (!summary?.totalRequests) return null
    const usage = formatModelUsage({
        inputTokens: summary.inputTokens,
        outputTokens: summary.outputTokens,
        totalTokens: summary.inputTokens + summary.outputTokens,
        cacheReadInputTokens: summary.cacheReadInputTokens,
        cacheCreationInputTokens: summary.cacheCreationInputTokens,
        hasInput: summary.inputReports > 0,
        missingInput: summary.inputReports < summary.totalRequests,
        hasOutput: summary.outputReports > 0,
        missingOutput: summary.outputReports < summary.totalRequests,
        hasCacheRead: summary.cacheReadReports > 0,
        hasCacheCreation: summary.cacheCreationReports > 0,
        missingCacheRead: summary.cacheReadReports < summary.totalRequests,
        missingCacheCreation: summary.cacheCreationReports < summary.totalRequests,
    })
    const ratio = summary.inputTokens > 0 && summary.inputReports === summary.totalRequests && summary.cacheReadReports === summary.totalRequests
        ? summary.cacheReadInputTokens / summary.inputTokens : null
    const cacheLabels = ratio == null ? [] : [`缓存率 ${percent.format(ratio)}`]
    const cacheDetails = ratio == null ? [] : [`缓存率：${percent.format(ratio)}`]
    const title = ['会话累计（已上报请求，包含所有 Agent）', usage.details, ...cacheDetails].filter(Boolean).join('\n')
    return <span className="composer-token-usage" title={title} aria-label={title}>
        {!summary.reportedRequests ? '用量未上报' : <TokenUsageBreakdown labels={[...usage.labels, ...cacheLabels]} />}
    </span>
}

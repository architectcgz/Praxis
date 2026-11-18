import type { ModelUsage } from '../../api'

export function formatRequestUsage(usage: ModelUsage) {
    return formatModelUsage({
        inputTokens: usage.inputTokens ?? 0,
        outputTokens: usage.outputTokens ?? 0,
        totalTokens: (usage.inputTokens ?? 0) + (usage.outputTokens ?? 0),
        cacheReadInputTokens: usage.cacheReadInputTokens ?? 0,
        cacheCreationInputTokens: usage.cacheCreationInputTokens ?? 0,
        hasInput: usage.inputTokens !== undefined,
        hasOutput: usage.outputTokens !== undefined,
        hasCacheRead: usage.cacheReadInputTokens !== undefined,
        hasCacheCreation: usage.cacheCreationInputTokens !== undefined,
        missingInput: usage.inputTokens === undefined,
        missingOutput: usage.outputTokens === undefined,
        missingCacheRead: usage.cacheReadInputTokens === undefined,
        missingCacheCreation: usage.cacheCreationInputTokens === undefined,
    })
}

const compactCount = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })
const exactCount = new Intl.NumberFormat('zh-CN')

/** 统一展示已上报分项，保留零值；不完整计数标为下限，缓存不重复计入总量。 */
function formatModelUsage(usage: {
    inputTokens: number
    outputTokens: number
    totalTokens: number
    cacheReadInputTokens: number
    cacheCreationInputTokens: number
    hasInput: boolean
    hasOutput: boolean
    hasCacheRead: boolean
    hasCacheCreation: boolean
    missingInput: boolean
    missingOutput: boolean
    missingCacheRead: boolean
    missingCacheCreation: boolean
}) {
    const cacheTokens = usage.cacheReadInputTokens + usage.cacheCreationInputTokens
    const hasCache = usage.hasCacheRead || usage.hasCacheCreation
    const missingCache = usage.missingCacheRead || usage.missingCacheCreation
    const compact = (value: number, missing = false) => `${compactCount.format(value)}${missing ? '+' : ''}`
    const exact = (value: number, missing = false) => `${exactCount.format(value)} tokens${missing ? '（部分计数未上报）' : ''}`
    return {
        labels: [
            usage.hasInput || usage.hasOutput ? `总计 ${compact(usage.totalTokens, usage.missingInput || usage.missingOutput)} tokens` : undefined,
            usage.hasInput ? `输入 ${compact(usage.inputTokens, usage.missingInput)}` : undefined,
            usage.hasOutput ? `输出 ${compact(usage.outputTokens, usage.missingOutput)}` : undefined,
            usage.hasCacheRead ? `缓存读取 ${compact(usage.cacheReadInputTokens, usage.missingCacheRead)}` : undefined,
            usage.hasCacheCreation ? `缓存写入 ${compact(usage.cacheCreationInputTokens, usage.missingCacheCreation)}` : undefined,
        ].filter((label) => label !== undefined),
        details: [
            usage.hasInput || usage.hasOutput ? `总计：${exact(usage.totalTokens, usage.missingInput || usage.missingOutput)}` : undefined,
            usage.hasInput ? `输入：${exact(usage.inputTokens, usage.missingInput)}（含缓存）` : undefined,
            usage.hasOutput ? `输出：${exact(usage.outputTokens, usage.missingOutput)}` : undefined,
            hasCache ? `缓存：${exact(cacheTokens, missingCache)}` : undefined,
            usage.hasCacheRead ? `缓存读取：${exact(usage.cacheReadInputTokens, usage.missingCacheRead)}` : undefined,
            usage.hasCacheCreation ? `缓存写入：${exact(usage.cacheCreationInputTokens, usage.missingCacheCreation)}` : undefined,
        ].filter(Boolean).join('\n'),
    }
}

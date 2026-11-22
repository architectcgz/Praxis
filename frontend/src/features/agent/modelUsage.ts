import type { ModelUsage } from '../../api'
import type { RequestActivities } from './history/entries'

export type UsageTotals = {
    inputTokens: number
    outputTokens: number
    totalTokens: number
    cacheReadInputTokens: number
    cacheCreationInputTokens: number
    hasInput: boolean
    hasOutput: boolean
    hasCacheRead: boolean
    hasCacheCreation: boolean
    hasUsage: boolean
    cacheRate: number | null
}

export const emptyUsageTotals: UsageTotals = {
    inputTokens: 0,
    outputTokens: 0,
    totalTokens: 0,
    cacheReadInputTokens: 0,
    cacheCreationInputTokens: 0,
    hasInput: false,
    hasOutput: false,
    hasCacheRead: false,
    hasCacheCreation: false,
    hasUsage: false,
    cacheRate: null,
}

const compactCount = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })
const exactCount = new Intl.NumberFormat('zh-CN')

export function formatCompactCount(value: number): string {
    return compactCount.format(value)
}

export function formatExactCount(value: number): string {
    return exactCount.format(value)
}

function computeCacheRate(inputTokens: number, cacheReadInputTokens: number, hasInput: boolean, hasCacheRead: boolean): number | null {
    if (!hasInput || !hasCacheRead || inputTokens <= 0) return null
    return Math.min(100, Math.round((cacheReadInputTokens / inputTokens) * 100))
}

/** 汇总单个 Agent 已记录的模型请求用量，缓存不重复计入总量。 */
export function summarizeRequestsUsage(requests: RequestActivities): UsageTotals {
    if (requests.size === 0) return emptyUsageTotals
    let inputTokens = 0
    let outputTokens = 0
    let cacheReadInputTokens = 0
    let cacheCreationInputTokens = 0
    let hasInput = false
    let hasOutput = false
    let hasCacheRead = false
    let hasCacheCreation = false

    for (const request of requests.values()) {
        const usage = request.result?.usage
        if (!usage) continue
        if (usage.inputTokens != null) {
            inputTokens += usage.inputTokens
            hasInput = true
        }
        if (usage.outputTokens != null) {
            outputTokens += usage.outputTokens
            hasOutput = true
        }
        if (usage.cacheReadInputTokens != null) {
            cacheReadInputTokens += usage.cacheReadInputTokens
            hasCacheRead = true
        }
        if (usage.cacheCreationInputTokens != null) {
            cacheCreationInputTokens += usage.cacheCreationInputTokens
            hasCacheCreation = true
        }
    }

    const hasUsage = hasInput || hasOutput || hasCacheRead || hasCacheCreation
    if (!hasUsage) return emptyUsageTotals
    return {
        inputTokens,
        outputTokens,
        totalTokens: inputTokens + outputTokens,
        cacheReadInputTokens,
        cacheCreationInputTokens,
        hasInput,
        hasOutput,
        hasCacheRead,
        hasCacheCreation,
        hasUsage,
        cacheRate: computeCacheRate(inputTokens, cacheReadInputTokens, hasInput, hasCacheRead),
    }
}

/** 合并多组独立累计用量（如会话总计 = 主代理 + 全部子代理）。 */
export function mergeUsageTotals(items: readonly UsageTotals[]): UsageTotals {
    let inputTokens = 0
    let outputTokens = 0
    let cacheReadInputTokens = 0
    let cacheCreationInputTokens = 0
    let hasInput = false
    let hasOutput = false
    let hasCacheRead = false
    let hasCacheCreation = false

    for (const item of items) {
        if (!item.hasUsage) continue
        inputTokens += item.inputTokens
        outputTokens += item.outputTokens
        cacheReadInputTokens += item.cacheReadInputTokens
        cacheCreationInputTokens += item.cacheCreationInputTokens
        hasInput ||= item.hasInput
        hasOutput ||= item.hasOutput
        hasCacheRead ||= item.hasCacheRead
        hasCacheCreation ||= item.hasCacheCreation
    }

    const hasUsage = hasInput || hasOutput || hasCacheRead || hasCacheCreation
    if (!hasUsage) return emptyUsageTotals
    return {
        inputTokens,
        outputTokens,
        totalTokens: inputTokens + outputTokens,
        cacheReadInputTokens,
        cacheCreationInputTokens,
        hasInput,
        hasOutput,
        hasCacheRead,
        hasCacheCreation,
        hasUsage,
        cacheRate: computeCacheRate(inputTokens, cacheReadInputTokens, hasInput, hasCacheRead),
    }
}

export function formatUsageDetails(usage: UsageTotals, title?: string): string {
    const exact = (value: number) => `${exactCount.format(value)} tokens`
    return [
        title,
        usage.hasInput || usage.hasOutput ? `总计：${exact(usage.totalTokens)}` : undefined,
        usage.hasInput ? `输入：${exact(usage.inputTokens)}（含缓存）` : undefined,
        usage.hasOutput ? `输出：${exact(usage.outputTokens)}` : undefined,
        usage.cacheRate !== null ? `缓存命中率：${usage.cacheRate}%` : undefined,
        usage.hasCacheRead ? `缓存读取：${exact(usage.cacheReadInputTokens)}` : undefined,
        usage.hasCacheCreation ? `缓存写入：${exact(usage.cacheCreationInputTokens)}` : undefined,
    ].filter(Boolean).join('\n')
}

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
    const cacheRate = computeCacheRate(usage.inputTokens, usage.cacheReadInputTokens, usage.hasInput, usage.hasCacheRead)
    const compact = (value: number, missing = false) => `${compactCount.format(value)}${missing ? '+' : ''}`
    const exact = (value: number, missing = false) => `${exactCount.format(value)} tokens${missing ? '（部分计数未上报）' : ''}`
    return {
        labels: [
            usage.hasInput || usage.hasOutput ? `总计 ${compact(usage.totalTokens, usage.missingInput || usage.missingOutput)}` : undefined,
            usage.hasInput ? `输入 ${compact(usage.inputTokens, usage.missingInput)}` : undefined,
            usage.hasOutput ? `输出 ${compact(usage.outputTokens, usage.missingOutput)}` : undefined,
            cacheRate !== null ? `缓存 ${cacheRate}%` : undefined,
            usage.hasCacheRead ? `缓存读 ${compact(usage.cacheReadInputTokens, usage.missingCacheRead)}` : undefined,
            usage.hasCacheCreation ? `缓存写 ${compact(usage.cacheCreationInputTokens, usage.missingCacheCreation)}` : undefined,
        ].filter((label) => label !== undefined),
        details: [
            usage.hasInput || usage.hasOutput ? `总计：${exact(usage.totalTokens, usage.missingInput || usage.missingOutput)}` : undefined,
            usage.hasInput ? `输入：${exact(usage.inputTokens, usage.missingInput)}（含缓存）` : undefined,
            usage.hasOutput ? `输出：${exact(usage.outputTokens, usage.missingOutput)}` : undefined,
            cacheRate !== null ? `缓存命中率：${cacheRate}%` : undefined,
            hasCache ? `缓存：${exact(cacheTokens, missingCache)}` : undefined,
            usage.hasCacheRead ? `缓存读取：${exact(usage.cacheReadInputTokens, usage.missingCacheRead)}` : undefined,
            usage.hasCacheCreation ? `缓存写入：${exact(usage.cacheCreationInputTokens, usage.missingCacheCreation)}` : undefined,
        ].filter(Boolean).join('\n'),
    }
}

import type { ModelUsageRecord } from '../../api/agents'

/** 按请求身份替换累计用量；查询返回的旧快照不得覆盖实时事件中的新计数。 */
export function mergeModelUsageRecords(current: ModelUsageRecord[], incoming: ModelUsageRecord[]): ModelUsageRecord[] {
    const key = (record: ModelUsageRecord) => JSON.stringify([record.sessionId, record.agentId, record.executionId, record.turnId])
    const records = new Map(current.map((record) => [key(record), record]))
    for (const record of incoming) {
        const previous = records.get(key(record))
        if (previous && (['inputTokens', 'outputTokens', 'cacheReadInputTokens', 'cacheCreationInputTokens'] as const).some((field) => (
            previous.usage[field] !== undefined && (record.usage[field] === undefined || record.usage[field]! < previous.usage[field]!)
        ))) continue
        records.set(key(record), record)
    }
    return [...records.values()]
}

/** 只累计已上报请求；缓存已包含在输入中，未知输出不补零。 */
export function summarizeModelUsage(records: ModelUsageRecord[]) {
    let inputTokens = 0
    let outputTokens = 0
    let cacheReadInputTokens = 0
    let cacheCreationInputTokens = 0
    let hasOutput = false
    let missingOutput = false
    let hasCacheRead = false
    let hasCacheCreation = false
    let missingCacheRead = false
    let missingCacheCreation = false
    for (const { usage } of records) {
        inputTokens += usage.inputTokens
        outputTokens += usage.outputTokens ?? 0
        cacheReadInputTokens += usage.cacheReadInputTokens ?? 0
        cacheCreationInputTokens += usage.cacheCreationInputTokens ?? 0
        hasOutput ||= usage.outputTokens !== undefined
        missingOutput ||= usage.outputTokens === undefined
        hasCacheRead ||= usage.cacheReadInputTokens !== undefined
        hasCacheCreation ||= usage.cacheCreationInputTokens !== undefined
        missingCacheRead ||= usage.cacheReadInputTokens === undefined
        missingCacheCreation ||= usage.cacheCreationInputTokens === undefined
    }
    return { inputTokens, outputTokens, totalTokens: inputTokens + outputTokens, cacheReadInputTokens, cacheCreationInputTokens, hasOutput, missingOutput, hasCacheRead, hasCacheCreation, missingCacheRead, missingCacheCreation }
}

const compactCount = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })
const exactCount = new Intl.NumberFormat('zh-CN')

/** 统一展示已上报分项，保留零值；不完整计数标为下限，缓存不重复计入总量。 */
export function formatModelUsage(usage: ReturnType<typeof summarizeModelUsage>) {
    const cacheTokens = usage.cacheReadInputTokens + usage.cacheCreationInputTokens
    const hasCache = usage.hasCacheRead || usage.hasCacheCreation
    const missingCache = usage.missingCacheRead || usage.missingCacheCreation
    const compact = (value: number, missing = false) => `${compactCount.format(value)}${missing ? '+' : ''}`
    const exact = (value: number, missing = false) => `${exactCount.format(value)} tokens${missing ? '（部分计数未上报）' : ''}`
    return {
        labels: [
            `总计 ${compact(usage.totalTokens, usage.missingOutput)} tokens`,
            `输入 ${compact(usage.inputTokens)}`,
            usage.hasOutput ? `输出 ${compact(usage.outputTokens, usage.missingOutput)}` : undefined,
            usage.hasCacheRead ? `缓存读取 ${compact(usage.cacheReadInputTokens, usage.missingCacheRead)}` : undefined,
            usage.hasCacheCreation ? `缓存写入 ${compact(usage.cacheCreationInputTokens, usage.missingCacheCreation)}` : undefined,
        ].filter((label) => label !== undefined),
        details: [
            `总计：${exact(usage.totalTokens, usage.missingOutput)}`,
            `输入：${exact(usage.inputTokens)}（含缓存）`,
            usage.hasOutput ? `输出：${exact(usage.outputTokens, usage.missingOutput)}` : undefined,
            hasCache ? `缓存：${exact(cacheTokens, missingCache)}` : undefined,
            usage.hasCacheRead ? `缓存读取：${exact(usage.cacheReadInputTokens, usage.missingCacheRead)}` : undefined,
            usage.hasCacheCreation ? `缓存写入：${exact(usage.cacheCreationInputTokens, usage.missingCacheCreation)}` : undefined,
        ].filter(Boolean).join('\n'),
    }
}

import type { ModelUsageRecord } from '../../api/agents'

/** 按请求身份替换累计用量；查询返回的旧快照不得覆盖实时事件中的新计数。 */
export function mergeModelUsageRecords(current: ModelUsageRecord[], incoming: ModelUsageRecord[]): ModelUsageRecord[] {
    const key = (record: ModelUsageRecord) => JSON.stringify([record.sessionId, record.agentId, record.turnId, record.step])
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

/** 统一会话与单次请求的分项展示；不完整计数标为下限，缓存不重复计入总量。 */
export function formatModelUsage(usage: ReturnType<typeof summarizeModelUsage>) {
    const cacheTokens = usage.cacheReadInputTokens + usage.cacheCreationInputTokens
    const hasCache = usage.hasCacheRead || usage.hasCacheCreation
    const missingCache = usage.missingCacheRead || usage.missingCacheCreation
    const compact = (value: number, reported = true, missing = false) => reported ? `${compactCount.format(value)}${missing ? '+' : ''}` : '未上报'
    const exact = (value: number, reported = true, missing = false) => reported ? `${exactCount.format(value)} tokens${missing ? '（部分计数未上报）' : ''}` : '未上报'
    return {
        labels: [
            `总计 ${compact(usage.totalTokens, true, usage.missingOutput)} tokens`,
            `输入 ${compact(usage.inputTokens)}`,
            `输出 ${compact(usage.outputTokens, usage.hasOutput, usage.missingOutput)}`,
            `缓存 ${compact(cacheTokens, hasCache, missingCache)}`,
        ],
        details: [
            `总计：${exact(usage.totalTokens, true, usage.missingOutput)}`,
            `输入：${exact(usage.inputTokens)}（含缓存）`,
            `输出：${exact(usage.outputTokens, usage.hasOutput, usage.missingOutput)}`,
            `缓存：${exact(cacheTokens, hasCache, missingCache)}`,
            `缓存读取：${exact(usage.cacheReadInputTokens, usage.hasCacheRead, usage.missingCacheRead)}`,
            `缓存写入：${exact(usage.cacheCreationInputTokens, usage.hasCacheCreation, usage.missingCacheCreation)}`,
        ].join('\n'),
    }
}

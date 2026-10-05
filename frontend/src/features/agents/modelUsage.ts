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
        missingOutput ||= usage.outputTokens === undefined
        hasCacheRead ||= usage.cacheReadInputTokens !== undefined
        hasCacheCreation ||= usage.cacheCreationInputTokens !== undefined
        missingCacheRead ||= usage.cacheReadInputTokens === undefined
        missingCacheCreation ||= usage.cacheCreationInputTokens === undefined
    }
    return { inputTokens, outputTokens, totalTokens: inputTokens + outputTokens, cacheReadInputTokens, cacheCreationInputTokens, missingOutput, hasCacheRead, hasCacheCreation, missingCacheRead, missingCacheCreation }
}

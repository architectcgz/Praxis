import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mergeModelUsageRecords, summarizeModelUsage } from '../src/features/agents/modelUsage.ts'

const record = (usage, step = 1, agentId = 'primary', sessionId = 'session') => ({ sessionId, agentId, turnId: agentId + '-turn', step, usage })

test('累计请求替换与旧查询合并不会重复计数，包含不同 step 和子 Agent', () => {
    const start = record({ inputTokens: 120, outputTokens: 0, cacheReadInputTokens: 80 })
    const completed = record({ ...start.usage, outputTokens: 30 })
    let records = mergeModelUsageRecords([], [start, completed, completed])
    records = mergeModelUsageRecords(records, [start, record({ inputTokens: 50, outputTokens: 10 }, 2), record({ inputTokens: 20, outputTokens: 5 }, 1, 'delegate')])
    assert.equal(records.length, 3)
    const usage = summarizeModelUsage(records)
    assert.equal(usage.inputTokens, 190)
    assert.equal(usage.outputTokens, 45)
    assert.equal(usage.totalTokens, 235)
    assert.equal(usage.cacheReadInputTokens, 80)
    assert.equal(usage.missingOutput, false)
})

test('缺失输出和缓存不能当成明确的零，旧查询不能丢失已知字段', () => {
    const known = record({ inputTokens: 20, outputTokens: 0, cacheReadInputTokens: 0 })
    const unknown = record({ inputTokens: 20 })
    assert.deepEqual(mergeModelUsageRecords([known], [unknown]), [known])
    assert.equal(summarizeModelUsage([unknown]).missingOutput, true)
    assert.equal(summarizeModelUsage([unknown]).hasCacheRead, false)
    assert.equal(summarizeModelUsage([known]).missingOutput, false)
    assert.equal(summarizeModelUsage([known]).hasCacheRead, true)
    assert.equal(mergeModelUsageRecords([known], [record(known.usage, 1, 'primary', 'another-session')]).length, 2)
})

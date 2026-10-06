import { getAgentBinding } from './bindings'

export type OperationTiming = {
    id: string
    parentId?: string
    sessionId: string
    agentId: string
    taskId: string
    kind: 'agent' | 'provider' | 'tool'
    name: string
    referenceId?: string
    startedAt: string
    finishedAt?: string
    durationMs?: number
    firstResponseMs?: number
    status: 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted'
    revision: number
}

/** 查询独立计时数据，不依赖消息正文；异常数据由 API 边界拒绝。 */
export async function listAgentTimings(agentId: string): Promise<OperationTiming[]> {
    const values: unknown = await getAgentBinding().ListAgentTimings(agentId)
    if (!Array.isArray(values)) throw new Error('计时数据格式无效。')
    return values.map(normalizeOperationTiming)
}

/** 校验持久化查询和实时事件共用的计时 DTO；缺省耗时与 0 毫秒保持区分。 */
export function normalizeOperationTiming(value: unknown): OperationTiming {
    if (typeof value !== 'object' || value === null) throw new Error('计时数据格式无效。')
    const record = value as Record<string, unknown>
    for (const key of ['id', 'sessionId', 'agentId', 'taskId', 'name', 'startedAt']) {
        if (typeof record[key] !== 'string' || !record[key]) throw new Error('计时数据格式无效。')
    }
    if (!['agent', 'provider', 'tool'].includes(String(record.kind)) ||
        !['running', 'completed', 'failed', 'cancelled', 'interrupted'].includes(String(record.status)) ||
        !Number.isFinite(Date.parse(record.startedAt as string)) ||
        !Number.isSafeInteger(record.revision) || Number(record.revision) < 1) throw new Error('计时数据格式无效。')
    for (const key of ['durationMs', 'firstResponseMs']) {
        if (record[key] != null && (typeof record[key] !== 'number' || !Number.isFinite(record[key]) || record[key] < 0)) throw new Error('计时数据格式无效。')
    }
    if ((record.status === 'running' || record.status === 'interrupted') && record.durationMs != null ||
        !['running', 'interrupted'].includes(String(record.status)) && record.durationMs == null ||
        record.firstResponseMs != null && (record.kind !== 'provider' || record.durationMs != null && Number(record.firstResponseMs) > Number(record.durationMs))) throw new Error('计时数据格式无效。')
    return {
        id: record.id as string,
        parentId: typeof record.parentId === 'string' ? record.parentId : undefined,
        sessionId: record.sessionId as string,
        agentId: record.agentId as string,
        taskId: record.taskId as string,
        kind: record.kind as OperationTiming['kind'],
        name: record.name as string,
        referenceId: typeof record.referenceId === 'string' ? record.referenceId : undefined,
        startedAt: record.startedAt as string,
        finishedAt: typeof record.finishedAt === 'string' ? record.finishedAt : undefined,
        durationMs: record.durationMs == null ? undefined : record.durationMs as number,
        firstResponseMs: record.firstResponseMs == null ? undefined : record.firstResponseMs as number,
        status: record.status as OperationTiming['status'],
        revision: record.revision as number,
    }
}

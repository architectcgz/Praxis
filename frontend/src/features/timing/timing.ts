import type { OperationTiming } from '../../api/timings'

/** 按记录版本合并查询与事件，终态不会被迟到的运行态覆盖。 */
export function mergeTimings(current: OperationTiming[], incoming: OperationTiming[]): OperationTiming[] {
    const records = new Map(current.map((record) => [record.id, record]))
    for (const record of incoming) {
        const previous = records.get(record.id)
        if (!previous || record.revision > previous.revision && previous.status === 'running') records.set(record.id, record)
    }
    return [...records.values()]
}

/** 使用毫秒、秒、分、时展示耗时；不伪造缺失数据。 */
export function formatDuration(milliseconds: number): string {
    if (!Number.isFinite(milliseconds) || milliseconds < 0) return '未知'
    if (milliseconds < 1000) return `${Math.floor(milliseconds)} ms`
    const seconds = milliseconds / 1000
    if (seconds < 60) return `${(Math.floor(seconds * 10) / 10).toFixed(1)} s`
    if (seconds < 3600) return `${Math.floor(seconds / 60)} 分 ${Math.floor(seconds % 60)} 秒`
    return `${Math.floor(seconds / 3600)} 时 ${Math.floor(seconds % 3600 / 60)} 分`
}

/** 关联消息与操作，重试分别保留；行内标签展示同一引用的最近一次尝试。 */
export function findTiming(records: OperationTiming[], taskId: string, kind: OperationTiming['kind'], referenceId?: string): OperationTiming | undefined {
    return records.filter((record) => record.taskId === taskId && record.kind === kind &&
        (referenceId === undefined || record.referenceId === referenceId))
        .sort((left, right) => Date.parse(right.startedAt) - Date.parse(left.startedAt))[0]
}

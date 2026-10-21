import { getCommandBinding } from './bindings'

export type QueuedTask = { id: string; prompt: string; sequence: number }

/** 校验 Session 查询返回的待办，不把执行状态混入输入模型。 */
export function normalizeQueuedTasks(value: unknown): QueuedTask[] {
    if (!Array.isArray(value)) throw new Error('任务队列数据格式无效。')
    return value.map((item: unknown) => {
        if (typeof item !== 'object' || item === null ||
            !('id' in item) || typeof item.id !== 'string' ||
            !('prompt' in item) || typeof item.prompt !== 'string' ||
            !('sequence' in item) || typeof item.sequence !== 'number' || !Number.isSafeInteger(item.sequence) || item.sequence < 1) {
            throw new Error('任务队列数据格式无效。')
        }
        return { id: item.id, prompt: item.prompt, sequence: item.sequence }
    })
}

export type CreateTaskRequest = {
    sessionId: string
    prompt: string
}

/** 创建 Session 输入并排队，Agent 与请求身份由后端调度时分配。 */
export function createTask(request: CreateTaskRequest) {
    return getCommandBinding().CreateTask(request)
}

export function cancelTask(sessionId: string, taskId: string) {
    return getCommandBinding().CancelTask({ sessionId, taskId })
}

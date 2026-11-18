import { getCommandBinding } from './bindings'
import type { QueuedTask as GeneratedQueuedTask } from '../../bindings/praxis/wails/bindings'

export type QueuedTask = Pick<GeneratedQueuedTask, 'id' | 'prompt' | 'sequence'>
export type CreateTaskRequest = {
    submissionId: string
    sessionId: string
    prompt: string
}

/** 创建 Session 输入并排队，Agent 与请求身份由后端调度时分配。 */
export function createTask(request: CreateTaskRequest) { return getCommandBinding().CreateTask(request) }
export function cancelTask(sessionId: string, taskId: string) { return getCommandBinding().CancelTask({ sessionId, taskId }) }

export function pauseQueue(sessionId: string) { return getCommandBinding().PauseQueue(sessionId) }
export function resumeQueue(sessionId: string) { return getCommandBinding().ResumeQueue(sessionId) }

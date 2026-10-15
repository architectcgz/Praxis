import type { AgentHistoryItem, AgentMessage, AgentSnapshot, SessionSnapshot, SessionSummary } from '../api'

export function agent(id = 'a1', sessionId = 's1', overrides: Partial<AgentSnapshot> = {}): AgentSnapshot {
    return { id, sessionId, name: id, definitionId: 'primary', profile: 'primary', state: 'idle', currentTaskId: '', taskIds: [], tasks: [], controlCommandIds: [], ...overrides }
}

export function message(sequence: number, overrides: Partial<AgentMessage> = {}): AgentHistoryItem {
    const at = new Date(Date.UTC(2026, 0, 1, 0, 0, sequence)).toISOString()
    return { kind: 'message', sequence, at, message: { id: `assistant:turn-${sequence}`, sequence, at, taskId: 't1', role: 'assistant', content: `message-${sequence}`, blocks: [], ...overrides } }
}

export function session(id = 's1', title = ''): SessionSnapshot {
    return { id, title, projectId: 'p1', workspaceId: 'w1', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', agents: [agent(`a-${id}`, id)] }
}

export function summary(id = 's1', updatedAt = '2026-01-01T00:00:00Z'): SessionSummary {
    const { agents: _agents, ...value } = session(id)
    return { ...value, updatedAt }
}

export function deferred<T>() {
    let resolve!: (value: T) => void
    let reject!: (error: unknown) => void
    const promise = new Promise<T>((accept, fail) => { resolve = accept; reject = fail })
    return { promise, resolve, reject }
}

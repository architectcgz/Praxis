import { isRequestEvent, type AgentEvent } from '../../api/events'
import { mergeStreamingEvent } from '../run/streaming'
import type { AgentEntry, SessionData, SessionOutput } from './sessionStore'
import { mergeMessages, mergeRequests } from '../agent/history/entries'

/** 接纳后端通知并更新对应 Agent 的展示状态。 */
export function applySessionEvent(data: SessionData, output: SessionOutput, event: AgentEvent): { data: SessionData; output: SessionOutput } {
    const update = (id: string, transform: (entry: AgentEntry) => AgentEntry) => {
        const entry = data.byId[id]
        if (entry) data = { ...data, byId: { ...data.byId, [id]: transform(entry) } }
    }
    if (event.kind === 'task_queue_changed') {
        if (data.session) data = { ...data, session: { ...data.session, tasks: event.data.tasks, queuePaused: event.data.paused } }
        return { data, output }
    }
    if (event.kind === 'session_config_reloaded') return { data: { ...data, configRevision: data.configRevision + 1 }, output }
    if (event.kind === 'session_title_changed' || event.kind === 'task_queue_storage_failed') return { data, output }
    const id = event.agentId
    const current = data.byId[id]
    if (event.kind === 'subagent_started') {
        if (current || data.endedRuns.has(event.runId)) return { data, output }
        data = {
            ...data,
            ids: [...data.ids, id],
            byId: {
                ...data.byId,
                [id]: {
                    agent: event.data.agent,
                    entries: event.data.messages,
                    requests: new Map(),
                    page: { hasMore: false, nextCursor: null },
                    loading: false,
                    error: '',
                },
            },
        }
        return { data, output }
    }
    if (!current) return { data, output }
    if (event.kind === 'entries_appended') {
        update(id, (entry) => ({ ...entry, entries: mergeMessages(entry.entries, event.data.messages) }))
        return { data, output }
    }
    if (event.kind === 'run_started' || event.kind === 'run_ending') {
        const run = event.data.run
        const active = current.agent.currentRun
        const recent = active || current.agent.latestRun
        const alreadyApplied = active?.ID === run.ID && (active.Status === run.Status || active.Status === 'ending')
        if (!alreadyApplied && !data.endedRuns.has(run.ID) && (!recent || recent.ID === run.ID || recent.StartedAt <= run.StartedAt)) {
            update(id, (entry) => ({
                ...entry,
                agent: { ...entry.agent, state: 'executing', currentRunId: run.ID, currentRun: run, UpdatedAt: run.StartedAt },
            }))
        }
        return { data, output }
    }
    if (event.kind === 'run_ended') {
        const { run, state, messages } = event.data
        const endedAt = run.Result?.EndedAt ?? run.StartedAt
        data = { ...data, endedRuns: new Set([...data.endedRuns, run.ID]) }
        update(id, (entry) => {
            const agent = entry.agent
            const newerRun = agent.currentRunId ? agent.currentRunId !== run.ID : (agent.latestRun?.Result?.EndedAt ?? '') > endedAt
            return {
                ...entry,
                entries: mergeMessages(entry.entries, messages),
                agent: newerRun ? agent : {
                    ...agent, state, currentRunId: '', currentRun: null, latestRun: run, UpdatedAt: endedAt,
                },
            }
        })
        if (output.streamingOutputs[id]?.runId === run.ID) {
            const streamingOutputs = { ...output.streamingOutputs }
            delete streamingOutputs[id]
            output = { streamingOutputs }
        }
        return { data, output }
    }
    if (event.kind === 'subagent_released') {
        if (data.selectedId !== id) update(id, clearHistory)
        return { data, output }
    }
    if (isRequestEvent(event)) {
        update(id, (entry) => ({ ...entry, requests: mergeRequests(entry.requests, [event.data]) }))
        if (event.data.requestKind !== 'iteration') return { data, output }
    }
    if (current.agent.currentRunId !== event.runId || data.endedRuns.has(event.runId)) return { data, output }
    if (event.kind === 'agent_state_changed') {
        update(id, (entry) => ({ ...entry, agent: { ...entry.agent, state: event.data.state } }))
        return { data, output }
    }
    if (event.kind === 'turn_started') return { data, output }
    if (event.kind === 'turn_completed' || event.kind === 'turn_failed' || event.kind === 'turn_interrupted') {
        const live = output.streamingOutputs[id]
        if (live) output = { streamingOutputs: { ...output.streamingOutputs, [id]: { ...live, turns: live.turns.filter((turn) => turn.turnId !== event.turnId) } } }
        return { data, output }
    }
    const streamingOutputs = mergeStreamingEvent(output.streamingOutputs, id, event)
    if (streamingOutputs !== output.streamingOutputs) output = { streamingOutputs }
    return { data, output }
}

export function clearHistory(entry: AgentEntry): AgentEntry {
    return { ...entry, entries: [], requests: new Map(), page: undefined, error: '', loading: false }
}

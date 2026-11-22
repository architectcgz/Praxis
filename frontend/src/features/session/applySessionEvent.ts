import { activeRunId } from '../../api/agents'
import { parseToolApproval } from '../../api/commands'
import { isRequestEvent, type AgentEvent } from '../../api/events'
import { normalizeModelOptions } from '../../api/models'
import { mergeStreamingEvent } from '../run/streaming'
import type { AgentEntry, SessionData, SessionOutput } from './sessionStore'
import { mergeMessages, mergeRequests } from '../agent/history/entries'
import { summarizeRequestsUsage } from '../agent/modelUsage'

/** 接纳后端通知并更新对应 Agent 的展示状态。 */
export function applySessionEvent(sessionId: string, data: SessionData, output: SessionOutput, event: AgentEvent): { data: SessionData; output: SessionOutput } {
    const update = (id: string, transform: (entry: AgentEntry) => AgentEntry) => {
        const entry = data.byId[id]
        if (entry) data = { ...data, byId: { ...data.byId, [id]: transform(entry) } }
    }
    if (event.kind === 'task_queue_changed') {
        if (data.session) data = { ...data, session: { ...data.session, tasks: event.data.tasks, queuePaused: event.data.paused } }
        return { data, output }
    }
    if (event.kind === 'session_config_reloaded') {
        if (data.session) {
            data = {
                ...data,
                session: {
                    ...data.session,
                    models: normalizeModelOptions(event.data.models),
                    modelSelection: event.data.modelSelection,
                    approvalMode: event.data.approvalMode,
                },
            }
        }
        return { data, output }
    }
    if (event.kind === 'session_title_changed') {
        if (data.session) {
            data = {
                ...data,
                session: { ...data.session, title: event.data.title, updatedAt: event.data.updatedAt },
            }
        }
        return { data, output }
    }
    if (event.kind === 'task_queue_storage_failed') return { data, output }
    const { agentId: id, runId } = event
    const current = data.byId[id]
    if (event.kind === 'tool_approval_required' && data.session && !data.endedRuns.has(runId)) {
        const nextApproval = parseToolApproval(event.data)
        const pendingApprovals = [
            ...data.session.pendingApprovals.filter(
                (item) => !(item.runId === nextApproval.runId && item.callId === nextApproval.callId),
            ),
            nextApproval,
        ]
        data = { ...data, session: { ...data.session, pendingApprovals } }
    }
    if (event.kind === 'tool_approval_resolved' && data.session) {
        const pendingApprovals = data.session.pendingApprovals.filter(
            (item) => !(item.runId === runId && item.callId === event.data.callId),
        )
        if (pendingApprovals.length !== data.session.pendingApprovals.length) {
            data = { ...data, session: { ...data.session, pendingApprovals } }
        }
    }
    if (event.kind === 'subagent_started') {
        if (data.ids.includes(id) || data.endedRuns.has(runId)) return { data, output }
        data = {
            ...data,
            ids: [...data.ids, id],
            byId: {
                ...data.byId,
                [id]: {
                    agent: {
                        ID: id, SessionID: sessionId, Kind: 'subagent', name: event.data.name,
                        state: 'executing', createdAt: event.data.createdAt, runId,
                    },
                    entries: mergeMessages(current?.entries ?? [], event.data.messages),
                    requests: current?.requests ?? new Map(),
                    page: current?.page ?? { hasMore: false, nextCursor: null },
                    loading: current?.loading ?? false,
                    error: current?.error ?? '',
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
    if (event.kind === 'run_started') {
        const { startedAt } = event.data
        const agent = current.agent
        const alreadyApplied = agent.runId === runId && agent.runStartedAt !== undefined
        if (!alreadyApplied && !data.endedRuns.has(runId) && (!agent.runStartedAt || agent.runStartedAt <= startedAt)) {
            update(id, (entry) => ({
                ...entry,
                agent: {
                    ...entry.agent, state: 'executing', runId, runStartedAt: startedAt,
                    runEndedAt: undefined, runOutcome: undefined, runFailure: undefined,
                    runIterations: undefined, runToolCalls: undefined,
                },
            }))
        }
        return { data, output }
    }
    if (event.kind === 'run_ended') {
        const { startedAt, endedAt, state, outcome, iterations, toolCalls, failureCode, failureMessage } = event.data
        const agent = current.agent
        const activeRun = activeRunId(agent)
        const newerRun = activeRun ? activeRun !== runId : (agent.runEndedAt ?? '') > endedAt
        data = { ...data, endedRuns: new Set([...data.endedRuns, runId]) }
        if (data.session) {
            const pendingApprovals = data.session.pendingApprovals.filter((item) => item.runId !== runId)
            if (pendingApprovals.length !== data.session.pendingApprovals.length) {
                data = { ...data, session: { ...data.session, pendingApprovals } }
            }
        }
        if (!newerRun) {
            const runFailure = failureMessage || failureCode
            update(id, (entry) => ({
                ...entry,
                agent: {
                    ...agent, state, runId, runStartedAt: startedAt, runEndedAt: endedAt, runOutcome: outcome, runFailure,
                    runIterations: iterations, runToolCalls: toolCalls,
                },
            }))
        }
        if (output.streamingOutputs[id]?.runId === runId) {
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
    if (activeRunId(current.agent) !== runId || data.endedRuns.has(runId)) return { data, output }
    if (event.kind === 'agent_state_changed') {
        update(id, (entry) => ({ ...entry, agent: { ...entry.agent, state: event.data.state } }))
        return { data, output }
    }
    if (event.kind === 'run_ending' || event.kind === 'turn_started') return { data, output }
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
    const archivedUsage = entry.requests.size > 0 ? summarizeRequestsUsage(entry.requests) : entry.archivedUsage
    return { ...entry, entries: [], requests: new Map(), archivedUsage, page: undefined, error: '', loading: false }
}

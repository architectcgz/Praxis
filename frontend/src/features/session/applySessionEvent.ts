import type { AgentEvent } from '../../api/events'
import { mergeStreamingEvent } from '../run/streaming'
import type { AgentEntry, SessionData, SessionOutput } from './sessionStore'
import { mergeEntries } from '../agent/history/entries'

/** 接纳已确定的通知；不请求后端，不推导 Run 状态，不启动业务。 */
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
    if (event.kind === 'subagent_started') {
        const agent = event.data.agent
        if (data.endedRuns.has(event.runId)) return { data, output }
        data = { ...data, runAgents:{...data.runAgents,[event.runId]:agent.ID},ids: data.ids.includes(agent.ID) ? data.ids : [...data.ids, agent.ID], byId: { ...data.byId, [agent.ID]: { agent, entries: event.data.entries, loading: false, error: '' } } }
        return { data, output }
    }
    if (event.kind === 'model_request_completed' || event.kind === 'model_request_failed' || event.kind === 'model_request_interrupted') {
        // 普通、审核和摘要请求都参与累计，聊天展示筛选不会影响用量。
        data = { ...data, usageRevision: data.usageRevision + 1 }
    }
    if (event.kind === 'entries_appended' || event.kind === 'run_started') data = { ...data,runAgents:{...data.runAgents,[event.runId]:event.data.agentId} }
    const id = data.runAgents[event.runId]
    const current = data.byId[id]
    if (!current) return { data, output }
    if (event.kind === 'entries_appended') {
        update(id, (entry) => ({ ...entry, entries: mergeEntries(entry.entries || [], event.data.entries) }))
        return { data, output }
    }
    if (event.kind === 'run_started') {
        const run = event.data.run
        const recent = current.agent.currentRun || current.agent.latestRun
        if (!data.endedRuns.has(run.ID) && (!recent || recent.ID===run.ID || recent.StartedAt <= run.StartedAt)) update(id, (entry) => ({ ...entry, agent: { ...entry.agent, state: 'executing', currentRunId: run.ID, currentRun: run, UpdatedAt: run.StartedAt } }))
        return { data, output }
    }
    if (event.kind === 'run_ended' || event.kind === 'request_canceled') {
        const { run, state, entries } = event.data
        data = { ...data, endedRuns: new Set([...data.endedRuns, run.ID]) }
        update(id, (entry) => ({
            ...entry,
            entries: mergeEntries(entry.entries || [], entries),
            agent: (entry.agent.currentRunId && entry.agent.currentRunId !== run.ID) || (entry.agent.latestRun && entry.agent.latestRun.ID !== run.ID && entry.agent.latestRun.EndedAt >= run.EndedAt) ? entry.agent : {
                ...entry.agent, state, currentRunId: '', currentRun: null, latestRun: run, UpdatedAt: run.EndedAt,
            },
        }))
        if (output.streamingOutputs[id]?.runId === run.ID) {
            const streamingOutputs = { ...output.streamingOutputs }
            delete streamingOutputs[id]
            output = { streamingOutputs }
        }
        return { data, output }
    }
    // 停止请求只表示命令已接收；执行状态由后端终态或明确状态通知更新。
    if (event.kind === 'subagent_released') {
        if (data.selectedId!==id) update(id,(entry)=>({...entry,entries:undefined,page:undefined,error:'',loading:false}))
        return {data,output}
    }
    if (event.kind === 'run_interrupt_requested') return { data, output }
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
    if (event.kind === 'error') data = { ...data, eventError: event.data.message }
    if (event.kind.startsWith('model_request_') && 'requestKind' in event.data && event.data.requestKind !== 'iteration') return { data, output }
    const streamingOutputs = mergeStreamingEvent(output.streamingOutputs, id, event)
    if (streamingOutputs !== output.streamingOutputs) output = { streamingOutputs }
    return { data, output }
}

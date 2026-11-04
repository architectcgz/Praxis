import { useCallback, useEffect, useMemo, useState, useSyncExternalStore, type Dispatch, type SetStateAction } from 'react'
import { subscribeAgentEvents, type Entry, type ApiErrorCode, type ModelOption, type SessionSnapshot } from '../../api'
import { createAgentStore } from './agentStore'
import { useAgentTaskInput } from './useAgentTaskInput'

type AgentDataOptions = {
    sessionID: string
    models: ModelOption[]
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
}
const emptyEntries: Entry[] = []

/** 页面首次接纳快照，之后由本页订阅持续更新；后台页面不停止接收事件。 */
export function useAgentData({ sessionID, models, setError, setErrorCode }: AgentDataOptions) {
    const [store] = useState(createAgentStore)
    const data = useSyncExternalStore(store.subscribeData, store.getData)
    const selected = data.byId[data.selectedId]
    const taskInput = useAgentTaskInput(sessionID, models, setError, setErrorCode)
    const { setInput } = taskInput

    const loadForSession = useCallback(async ({ agents, id, branch, entries }: SessionSnapshot) => {
        const scope = store.getScope()
        if (scope.sessionId !== id || scope.branchId !== branch.id) store.reset()
        store.setScope(id, branch.id)
        store.setRoster(agents)
        for (const agent of agents) store.loaded(agent.ID, agent, entries[agent.ID])
    }, [store])

    const selectAgent = useCallback((id: string) => {
        if (!store.getData().byId[id]) return
        if (store.getSelectedAgentID() !== id) { store.select(id); setInput('') }
        setError('')
        setErrorCode(undefined)
    }, [store, setInput, setError, setErrorCode])

    useEffect(() => subscribeAgentEvents(store.receive), [store])
    const agents = useMemo(() => data.ids.map((id) => data.byId[id].agent), [data.ids, data.byId])
    const agentEntries = useMemo(() => Object.fromEntries(data.ids.map((id) => [id, data.byId[id].entries || emptyEntries])), [data.ids, data.byId])
    const agentEntriesErrors = useMemo(() => Object.fromEntries(data.ids.filter((id) => data.byId[id].error).map((id) => [id, data.byId[id].error])), [data.ids, data.byId])
    const inputActions = useMemo(() => ({ beginInput: store.beginInput, acceptInput: store.acceptInput, rejectInput: store.rejectInput, stopAwaiting: store.stopAwaiting }), [store])
    const outputStore = useMemo(() => ({ subscribeOutput: store.subscribeOutput, getOutput: store.getOutput }), [store])
    return {
        ...taskInput,
        agent: selected?.agent || null, agents, agentEntries, agentEntriesErrors,
        agentLoading: selected?.loading ?? false,
        entries: selected?.entries || emptyEntries,
        entriesError: selected?.error || '',
        selectedAgentID: data.selectedId, getSelectedAgentID: store.getSelectedAgentID,
        outputStore, inputActions, loadForSession, selectAgent,
    }
}

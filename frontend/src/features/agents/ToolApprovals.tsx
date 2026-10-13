import { useCallback, useEffect, useState } from 'react'
import { approveToolCall, denyToolCall, listToolApprovals, subscribeAgentEvents, type ToolApproval } from '../../api'
import { readableError } from '../../shared/errors'

/** 审批请求从持久化调用状态读取，重连后仍可批准同一个调用。 */
export function ToolApprovals({ agentId }: { agentId: string }) {
    const [requests, setRequests] = useState<ToolApproval[]>([])
    const [busy, setBusy] = useState('')
    const [error, setError] = useState('')
    const reload = useCallback(async () => {
        setRequests(await listToolApprovals(agentId))
    }, [agentId])
    useEffect(() => {
        let active = true
        let generation = 0
        const refresh = async () => {
            const current = ++generation
            try {
                const values = await listToolApprovals(agentId)
                if (active && current === generation) { setRequests(values); setError('') }
            } catch (err) {
                if (active && current === generation) setError(readableError(err))
            }
        }
        void refresh()
        const unsubscribe = subscribeAgentEvents((event) => {
            if (event.agentId === agentId && ['tool_approval_required', 'tool_result', 'task_ended', 'request_canceled'].includes(event.kind)) void refresh()
        })
        return () => { active = false; unsubscribe() }
    }, [agentId])
    const decide = async (request: ToolApproval, approve: boolean) => {
        setBusy(request.callId)
        setError('')
        try {
            if (approve) await approveToolCall(request)
            else await denyToolCall(request)
            await reload()
        } catch (err) { setError(readableError(err)) }
        finally { setBusy('') }
    }
    if (requests.length === 0 && !error) return null
    return <section className="tool-approvals" aria-label="待审批的工具调用">
        {error && <p role="alert">{error}</p>}
        {requests.map((request) => <article key={`${request.taskId}:${request.callId}`}>
            <strong>工具调用需要确认：{request.name}</strong>
            <pre>{JSON.stringify(request.arguments, null, 2)}</pre>
            <button type="button" disabled={busy !== ''} onClick={() => void decide(request, true)}>批准执行</button>
            <button type="button" disabled={busy !== ''} onClick={() => void decide(request, false)}>拒绝</button>
        </article>)}
    </section>
}

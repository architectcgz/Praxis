import { useCallback, useEffect, useRef, useState } from 'react'
import { approveToolCall, denyToolCall, listToolApprovals, subscribeAgentEvents, type ToolApproval } from '../../api'
import { readableError } from '../../shared/errors'

/** 审批请求从持久化调用状态读取，重连后仍可批准同一个调用。 */
export function ToolApprovals({ agentId }: { agentId: string }) {
    const [requests, setRequests] = useState<ToolApproval[]>([])
    const [busy, setBusy] = useState('')
    const [error, setError] = useState('')
    const requestGeneration = useRef(0)
    const reload = useCallback(async () => {
        const current = ++requestGeneration.current
        try {
            const values = await listToolApprovals(agentId)
            if (current === requestGeneration.current) {
                setRequests(values)
                setError('')
            }
        } catch (err) {
            if (current === requestGeneration.current) setError(readableError(err))
        }
    }, [agentId])
    useEffect(() => {
        const unsubscribe = subscribeAgentEvents((event) => {
            if (event.agentId === agentId && ['tool_approval_required', 'tool_result', 'task_ended', 'request_canceled'].includes(event.kind)) void reload()
        })
        void reload()
        return () => { requestGeneration.current += 1; unsubscribe() }
    }, [agentId, reload])
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
            <strong>命令需要审核：{request.name}</strong>
            <pre>{request.command}</pre>
            <p>工作目录：{request.path}</p>
            <p>超时：{request.timeout} 秒 · 审核规则：{request.ruleId === 'default' ? '默认审核' : request.ruleId}</p>
            <button type="button" disabled={busy !== ''} onClick={() => void decide(request, true)}>批准执行</button>
            <button type="button" disabled={busy !== ''} onClick={() => void decide(request, false)}>拒绝</button>
        </article>)}
    </section>
}

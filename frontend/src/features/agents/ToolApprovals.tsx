import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { AlertCircle, Check, LoaderCircle, ShieldCheck, X } from 'lucide-react'
import { approveToolCall, denyToolCall, listToolApprovals, subscribeAgentEvents, type ToolApproval } from '../../api'
import { readableError } from '../../shared/errors'

type ApprovalState = {
    pendingApprovals: ToolApproval[]
    busy: string
    error: string
    reload: () => Promise<void>
    decide: (pendingApproval: ToolApproval, approve: boolean) => Promise<void>
}

const ToolApprovalsContext = createContext<ApprovalState | null>(null)

/** 会话内只查询和订阅一次审批状态，由对应的命令卡片展示；重连后仍可审核同一个调用。 */
export function ToolApprovalsProvider({ agentId, children }: { agentId: string; children: ReactNode }) {
    const [pendingApprovals, setPendingApprovals] = useState<ToolApproval[]>([])
    const [busy, setBusy] = useState('')
    const [error, setError] = useState('')
    const requestGeneration = useRef(0)
    const reload = useCallback(async () => {
        const current = ++requestGeneration.current
        try {
            const values = await listToolApprovals(agentId)
            if (current === requestGeneration.current) {
                setPendingApprovals(values)
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
    const decide = async (pendingApproval: ToolApproval, approve: boolean) => {
        setBusy(pendingApproval.callId)
        setError('')
        try {
            if (approve) await approveToolCall(pendingApproval)
            else await denyToolCall(pendingApproval)
            await reload()
        } catch (err) { setError(readableError(err)) }
        finally { setBusy('') }
    }
    return <ToolApprovalsContext value={{ pendingApprovals, busy, error, reload, decide }}>{children}</ToolApprovalsContext>
}

/** 按任务与调用身份关联审核，避免把其他命令的批准操作挂到当前卡片。 */
export function useToolApproval(taskId: string, callId?: string) {
    const approvals = useContext(ToolApprovalsContext)
    return approvals ? { ...approvals, pendingApproval: approvals.pendingApprovals.find((pendingApproval) => pendingApproval.taskId === taskId && pendingApproval.callId === callId) } : null
}

/** 审核操作嵌入工具卡片，命令与工作目录由同一卡片的参数区完整展示。 */
export function ToolApprovalReview({ approval }: { approval: NonNullable<ReturnType<typeof useToolApproval>> }) {
    const { pendingApproval, busy, error, reload, decide } = approval
    return <section className="tool-approval" aria-label="命令执行审核" aria-busy={!!pendingApproval && busy === pendingApproval.callId}>
        <div className="tool-approval-header">
            <ShieldCheck size={16} aria-hidden="true" />
            <strong>执行前确认</strong>
            <span>批准后将立即执行</span>
        </div>
        {pendingApproval && <dl className="tool-approval-details">
            <div><dt>审核规则</dt><dd>{pendingApproval.ruleId === 'default' ? '默认审核' : pendingApproval.ruleId}</dd></div>
        </dl>}
        {error && <p className="tool-approval-error" role="alert">
            <AlertCircle size={16} aria-hidden="true" /><span>{error}</span>
        </p>}
        {pendingApproval ? <div className="tool-approval-footer">
            <span className="tool-approval-hint" role="status">
                {busy === pendingApproval.callId ? <><LoaderCircle className="is-spinning" size={14} aria-hidden="true" />正在提交审核…</> : '仅授权本次调用'}
            </span>
            <div className="tool-approval-actions">
                <button className="tool-approval-deny" type="button" disabled={busy !== ''} onClick={() => void decide(pendingApproval, false)}>
                    <X size={14} aria-hidden="true" />拒绝
                </button>
                <button className="tool-approval-approve" type="button" disabled={busy !== ''} onClick={() => void decide(pendingApproval, true)}>
                    <Check size={14} aria-hidden="true" />批准执行
                </button>
            </div>
        </div> : <button className="output-button" type="button" onClick={() => void reload()}>重新加载审核</button>}
    </section>
}

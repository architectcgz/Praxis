import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { AlertCircle, Check, LoaderCircle, ShieldAlert, X } from 'lucide-react'
import { approveToolCall, denyToolCall, type ToolApproval } from '../../api'
import { readableError } from '../../shared/errors'

type ApprovalState = {
    pendingApprovals: ToolApproval[]
    busy: string
    decisionError: string
    decide: (pendingApproval: ToolApproval, approve: boolean) => Promise<void>
}

const ToolApprovalsContext = createContext<ApprovalState | null>(null)

/** 待审批列表由会话初始快照与实时事件推送维护，此处只承载审批决策提交。 */
export function ToolApprovalsProvider({ pendingApprovals, children }: { pendingApprovals: ToolApproval[]; children: ReactNode }) {
    const [busy, setBusy] = useState('')
    const [decisionError, setDecisionError] = useState('')
    const decide = useCallback(async (pendingApproval: ToolApproval, approve: boolean) => {
        setBusy(pendingApproval.callId)
        setDecisionError('')
        try {
            if (approve) await approveToolCall(pendingApproval)
            else await denyToolCall(pendingApproval)
        } catch (err) { setDecisionError(readableError(err)) }
        finally { setBusy('') }
    }, [])
    const value = useMemo(() => ({ pendingApprovals, busy, decisionError, decide }), [pendingApprovals, busy, decisionError, decide])
    return <ToolApprovalsContext value={value}>{children}</ToolApprovalsContext>
}

/** 历史消息不读取当前审批状态；实时审批闸口仍由外层 Provider 维护。 */
export function ToolApprovalScope({ historical, children }: { historical: boolean; children: ReactNode }) {
    const approvals = useContext(ToolApprovalsContext)
    return <ToolApprovalsContext value={historical ? null : approvals}>{children}</ToolApprovalsContext>
}

/** 按执行与调用身份关联审核，供活动账本读取后端冻结的参数。 */
export function useToolApproval(runId: string, callId?: string) {
    const approvals = useContext(ToolApprovalsContext)
    return approvals ? { ...approvals, pendingApproval: approvals.pendingApprovals.find((pendingApproval) => pendingApproval.runId === runId && pendingApproval.callId === callId) } : null
}

/** 读取当前 Provider 作用域内的审批列表与决策动作，供总览面板就地审批复用。 */
export function useToolApprovals() {
    return useContext(ToolApprovalsContext)
}

/** 审批决定只在输入区上方的闸口处理，提交失败单独展示。 */
export function ApprovalGate() {
    const approvals = useContext(ToolApprovalsContext)
    const pending = approvals?.pendingApprovals[0]
    if (!approvals || (!pending && !approvals.decisionError)) return null
    const total = approvals.pendingApprovals.length
    const busy = Boolean(pending && approvals.busy === pending.callId)
    const disabled = approvals.busy !== ''
    const path = pending?.arguments.path
    const summary = pending?.name === 'bash' ? pending.arguments.command : path
    const readRange = pending?.name === 'read_file' && (pending.arguments.offset !== undefined || pending.arguments.limit !== undefined)
        ? [pending.arguments.offset !== undefined ? `起始行 ${pending.arguments.offset}` : '', pending.arguments.limit !== undefined ? `最多 ${pending.arguments.limit} 行` : ''].filter(Boolean).join(' · ')
        : ''
    return <section className="approval-gate" aria-label="当前执行的审批" aria-busy={busy}>
        <div className="approval-gate-box">
            <div className="approval-gate-head">
                <ShieldAlert size={13} aria-hidden="true" />
                <strong>{pending ? '需要批准' : '当前执行的审批'}</strong>
                <span className="approval-gate-note" title={summary}>{summary}</span>
                {total > 1 && <span className="approval-gate-count">{1} / {total}</span>}
            </div>
            {pending && <dl className="approval-gate-details">
                <div><dt>工具</dt><dd><code>{pending.name}</code></dd></div>
                {path && <div><dt>操作路径</dt><dd><code>{path}</code></dd></div>}
                {readRange && <div><dt>读取范围</dt><dd>{readRange}</dd></div>}
                <div><dt>命中规则</dt><dd>{pending.ruleId === 'default' ? '项目或全局默认' : pending.ruleId}</dd></div>
                <div><dt>处理方式</dt><dd>{pending.action === 'auto' ? '模型不确定，转人工' : '必须人工确认'}</dd></div>
                {pending.riskVerdict && <div><dt>模型审核</dt><dd>{pending.riskReason || pending.riskVerdict}</dd></div>}
            </dl>}
            {pending?.name === 'bash' && (
                <pre className="instrument approval-gate-preview" aria-label="待执行完整命令">
                    <span className="output-command-prompt" aria-hidden="true">$</span>
                    {pending.arguments.command}
                </pre>
            )}
            {pending?.name === 'apply_patch' && pending.arguments.patch && (
                <pre className="instrument approval-gate-preview" aria-label="待执行补丁预览">
                    {pending.arguments.patch}
                </pre>
            )}
            {approvals.decisionError && <p className="approval-gate-error" role="alert">
                <AlertCircle size={13} aria-hidden="true" /><span>审核提交失败：{approvals.decisionError}</span>
            </p>}
            {pending && <div className="approval-gate-foot">
                <span className="approval-gate-hint" role="status">
                    {busy ? <><LoaderCircle className="is-spinning" size={13} aria-hidden="true" />正在提交审核…</> : '仅授权本次调用'}
                </span>
                <button className="approval-gate-deny" type="button" disabled={disabled} onClick={() => void approvals.decide(pending, false)}>
                    <X size={13} aria-hidden="true" />拒绝
                </button>
                <button className="approval-gate-approve" type="button" disabled={disabled} onClick={() => void approvals.decide(pending, true)}>
                    <Check size={13} aria-hidden="true" />批准执行
                </button>
            </div>}
        </div>
    </section>
}

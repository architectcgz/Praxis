import { FormEvent, useState } from 'react'
import { X } from 'lucide-react'
import { Overlay } from '../../components/ui'
import { API_ERROR_CODES, createSession } from '../../api'
import { readableError } from '../../shared/errors'

type NewSessionDialogProps = {
    projectName: string
    projectID: string
    workspaceID: string
    bridgeAvailable: boolean
    onCreated: (sessionID: string) => void
    onClose: () => void
    onOpenSettings: () => void
}

export function NewSessionDialog({
    projectName,
    projectID,
    workspaceID,
    bridgeAvailable,
    onCreated,
    onClose,
    onOpenSettings,
}: NewSessionDialogProps) {
    const [sessionGoal, setSessionGoal] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const [modelConfigError, setModelConfigError] = useState(false)

    const close = () => {
        if (!busy) {
            onClose()
        }
    }

    const submit = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault()
        const goal = sessionGoal.trim()
        if (busy || !projectID || !workspaceID || !goal || !bridgeAvailable) {
            return
        }
        setBusy(true)
        setError('')
        setModelConfigError(false)
        try {
            const created = await createSession({
                projectId: projectID,
                workspaceId: workspaceID,
                goal,
                requestId: crypto.randomUUID(),
            })
            onCreated(created.sessionId)
            onClose()
        } catch (err) {
            setError(readableError(err))
            setModelConfigError(err instanceof Error && err.message === API_ERROR_CODES.modelNotConfigured)
        } finally {
            setBusy(false)
        }
    }

    return (
        <Overlay labelledBy="new-session-title" onClose={close}>
            <section
                className="project-dialog"
            >
                <div className="dialog-heading">
                    <div>
                        <span className="section-label">{projectName}</span>
                        <h2 id="new-session-title">新建会话</h2>
                    </div>
                    <button
                        className="icon-button dialog-close"
                        type="button"
                        title="关闭"
                        aria-label="关闭新建会话对话框"
                        onClick={close}
                        disabled={busy}
                    >
                        <X size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>
                {error && (
                    <div className="dialog-error" role="alert">
                        <span>{error}</span>
                        {modelConfigError && (
                            <button className="dialog-error-action" type="button" onClick={onOpenSettings}>打开设置</button>
                        )}
                    </div>
                )}
                <form className="project-form" onSubmit={(event) => void submit(event)}>
                    <label htmlFor="session-goal">会话目标</label>
                    <input
                        id="session-goal"
                        value={sessionGoal}
                        onChange={(event) => setSessionGoal(event.target.value)}
                        placeholder="主代理应该做什么？"
                        autoComplete="off"
                        autoFocus
                        required
                        disabled={busy}
                    />
                    <div className="modal-actions">
                        <button className="modal-secondary" type="button" onClick={close} disabled={busy}>
                            取消
                        </button>
                        <button className="modal-primary" type="submit" disabled={busy || !sessionGoal.trim() || !bridgeAvailable}>
                            {busy ? '创建中...' : '创建会话'}
                        </button>
                    </div>
                </form>
            </section>
        </Overlay>
    )
}

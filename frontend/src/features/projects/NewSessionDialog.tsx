import {FormEvent, useState} from 'react'
import {X} from 'lucide-react'
import {createSession} from '../../shared/api'
import {readableError} from './errors'

type NewSessionDialogProps = {
    projectName: string
    workspaceKey: string
    bridgeAvailable: boolean
    onCreated: (sessionID: string) => void
    onClose: () => void
}

export function NewSessionDialog({
    projectName,
    workspaceKey,
    bridgeAvailable,
    onCreated,
    onClose,
}: NewSessionDialogProps) {
    const [sessionGoal, setSessionGoal] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')

    const close = () => {
        if (!busy) {
            onClose()
        }
    }

    const submit = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault()
        if (busy || !workspaceKey || !bridgeAvailable) {
            return
        }
        setBusy(true)
        setError('')
        try {
            const created = await createSession({
                workspaceKey,
                goal: sessionGoal.trim(),
            })
            onCreated(created.sessionId)
            onClose()
        } catch (err) {
            setError(readableError(err))
        } finally {
            setBusy(false)
        }
    }

    return (
        <div
            className="modal-backdrop"
            role="presentation"
            onMouseDown={(event) => {
                if (event.target === event.currentTarget) {
                    close()
                }
            }}
        >
            <section
                className="project-dialog"
                role="dialog"
                aria-modal="true"
                aria-labelledby="new-session-title"
                onKeyDown={(event) => {
                    if (event.key === 'Escape') {
                        close()
                    }
                }}
            >
                <div className="dialog-heading">
                    <div>
                        <span className="section-label">{projectName}</span>
                        <h2 id="new-session-title">New session</h2>
                    </div>
                    <button
                        className="icon-button dialog-close"
                        type="button"
                        title="Close"
                        aria-label="Close new session dialog"
                        onClick={close}
                        disabled={busy}
                    >
                        <X size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>
                {error && <div className="dialog-error" role="alert">{error}</div>}
                <form className="project-form" onSubmit={(event) => void submit(event)}>
                    <label htmlFor="session-goal">Session goal <span>(optional)</span></label>
                    <input
                        id="session-goal"
                        value={sessionGoal}
                        onChange={(event) => setSessionGoal(event.target.value)}
                        placeholder="What should the Primary Agent work on?"
                        autoComplete="off"
                        autoFocus
                        disabled={busy}
                    />
                    <div className="modal-actions">
                        <button className="modal-secondary" type="button" onClick={close} disabled={busy}>
                            Cancel
                        </button>
                        <button className="modal-primary" type="submit" disabled={busy || !bridgeAvailable}>
                            {busy ? 'Creating...' : 'Create session'}
                        </button>
                    </div>
                </form>
            </section>
        </div>
    )
}

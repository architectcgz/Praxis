import {FormEvent, useState} from 'react'
import {X} from 'lucide-react'
import {createProject} from '../../shared/api'
import {readableError} from './errors'

type NewProjectDialogProps = {
    bridgeAvailable: boolean
    onCreated: (sessionID: string) => void
    onClose: () => void
}

export function NewProjectDialog({
    bridgeAvailable,
    onCreated,
    onClose,
}: NewProjectDialogProps) {
    const [projectName, setProjectName] = useState('')
    const [projectGoal, setProjectGoal] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')

    const close = () => {
        if (!busy) {
            onClose()
        }
    }

    const submit = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault()
        const name = projectName.trim()
        if (!name || busy || !bridgeAvailable) {
            return
        }
        setBusy(true)
        setError('')
        try {
            const created = await createProject({
                projectName: name,
                goal: projectGoal.trim(),
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
                aria-labelledby="new-project-title"
                onKeyDown={(event) => {
                    if (event.key === 'Escape') {
                        close()
                    }
                }}
            >
                <div className="dialog-heading">
                    <div>
                        <span className="section-label">Projects</span>
                        <h2 id="new-project-title">New project</h2>
                    </div>
                    <button
                        className="icon-button dialog-close"
                        type="button"
                        title="Close"
                        aria-label="Close new project dialog"
                        onClick={close}
                        disabled={busy}
                    >
                        <X size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>
                {error && <div className="dialog-error" role="alert">{error}</div>}
                <form className="project-form" onSubmit={(event) => void submit(event)}>
                    <label htmlFor="project-name">Project name</label>
                    <input
                        id="project-name"
                        value={projectName}
                        onChange={(event) => setProjectName(event.target.value)}
                        placeholder="my-project"
                        autoComplete="off"
                        autoFocus
                        required
                        disabled={busy}
                    />
                    <label htmlFor="project-goal">First session goal <span>(optional)</span></label>
                    <input
                        id="project-goal"
                        value={projectGoal}
                        onChange={(event) => setProjectGoal(event.target.value)}
                        placeholder="What should the Primary Agent work on?"
                        disabled={busy}
                    />
                    <div className="modal-actions">
                        <button className="modal-secondary" type="button" onClick={close} disabled={busy}>
                            Cancel
                        </button>
                        <button className="modal-primary" type="submit" disabled={busy || !projectName.trim() || !bridgeAvailable}>
                            {busy ? 'Creating...' : 'Create project'}
                        </button>
                    </div>
                </form>
            </section>
        </div>
    )
}

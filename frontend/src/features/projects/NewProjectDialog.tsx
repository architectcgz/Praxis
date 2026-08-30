import { FormEvent, useState } from 'react'
import { X } from 'lucide-react'
import { Overlay } from '../../components/ui'
import { createProject } from '../../api'
import { readableError } from '../../shared/errors'

type NewProjectDialogProps = {
    bridgeAvailable: boolean
    onCreated: (projectID: string) => void
    onClose: () => void
}

export function NewProjectDialog({
    bridgeAvailable,
    onCreated,
    onClose,
}: NewProjectDialogProps) {
    const [projectName, setProjectName] = useState('')
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
            })
            onCreated(created.projectId)
            onClose()
        } catch (err) {
            setError(readableError(err))
        } finally {
            setBusy(false)
        }
    }

    return (
        <Overlay labelledBy="new-project-title" onClose={close}>
            <section
                className="project-dialog"
            >
                <div className="dialog-heading">
                    <div>
                        <span className="section-label">项目</span>
                        <h2 id="new-project-title">新建项目</h2>
                    </div>
                    <button
                        className="icon-button dialog-close"
                        type="button"
                        title="关闭"
                        aria-label="关闭新建项目对话框"
                        onClick={close}
                        disabled={busy}
                    >
                        <X size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>
                {error && (
                    <div className="dialog-error" role="alert">
                        <span>{error}</span>
                    </div>
                )}
                <form className="project-form" onSubmit={(event) => void submit(event)}>
                    <label htmlFor="project-name">项目名称</label>
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
                    <div className="modal-actions">
                        <button className="modal-secondary" type="button" onClick={close} disabled={busy}>
                            取消
                        </button>
                        <button className="modal-primary" type="submit" disabled={busy || !projectName.trim() || !bridgeAvailable}>
                            {busy ? '创建中...' : '创建项目'}
                        </button>
                    </div>
                </form>
            </section>
        </Overlay>
    )
}

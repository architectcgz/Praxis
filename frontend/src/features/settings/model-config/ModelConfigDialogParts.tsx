import type { ReactNode } from 'react'
import { AlertCircle, Check, LoaderCircle, Trash2 } from 'lucide-react'

type FieldProps = {
    label: string
    children: ReactNode
}

export function Field({ label, children }: FieldProps) {
    return (
        <label className="config-field">
            <span>{label}</span>
            {children}
        </label>
    )
}

type DialogActionsProps = {
    saving: boolean
    onClose: () => void
    onDelete?: () => void
    confirmDelete: boolean
    setConfirmDelete: (value: boolean) => void
    deleteDetail: string
}

export function DialogActions({
    saving,
    onClose,
    onDelete,
    confirmDelete,
    setConfirmDelete,
    deleteDetail,
}: DialogActionsProps) {
    return (
        <>
            {confirmDelete && (
                <div className="config-delete-confirm" role="alert">
                    <AlertCircle size={16} />
                    <span>{deleteDetail}</span>
                    <button type="button" disabled={saving} onClick={onDelete}>
                        确认删除
                    </button>
                </div>
            )}
            <div className="modal-actions config-actions">
                {onDelete && (
                    <button
                        className="config-delete"
                        type="button"
                        disabled={saving}
                        title="删除"
                        onClick={() => setConfirmDelete(!confirmDelete)}
                    >
                        <Trash2 size={15} aria-hidden="true" />
                        <span>删除</span>
                    </button>
                )}
                <span className="config-action-spacer" />
                <button
                    className="modal-secondary"
                    type="button"
                    disabled={saving}
                    onClick={onClose}
                >
                    取消
                </button>
                <button className="modal-primary" type="submit" disabled={saving}>
                    {saving ? (
                        <LoaderCircle className="is-spinning" size={15} />
                    ) : (
                        <Check size={15} />
                    )}
                    <span>保存</span>
                </button>
            </div>
        </>
    )
}

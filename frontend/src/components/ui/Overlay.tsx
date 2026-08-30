import { ReactNode, useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'

type OverlayProps = {
    labelledBy: string
    onClose: () => void
    children: ReactNode
}

/** L4 modal portal with backdrop and keyboard dismissal. */
export function Overlay({ labelledBy, onClose, children }: OverlayProps) {
    const closeRef = useRef(onClose)

    useEffect(() => {
        closeRef.current = onClose
    }, [onClose])

    // Keep the listener stable across dialog body renders.
    useEffect(() => {
        const closeOnEscape = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                closeRef.current()
            }
        }
        window.addEventListener('keydown', closeOnEscape)
        return () => window.removeEventListener('keydown', closeOnEscape)
    }, [])

    const host = document.getElementById('overlay-root')
    if (!host) {
        return null
    }

    return createPortal(
        <div
            className="modal-backdrop"
            role="dialog"
            aria-modal="true"
            aria-labelledby={labelledBy}
            onMouseDown={(event) => {
                if (event.target === event.currentTarget) {
                    closeRef.current()
                }
            }}
        >
            {children}
        </div>,
        host,
    )
}

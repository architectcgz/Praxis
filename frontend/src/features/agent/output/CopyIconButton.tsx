import { useEffect, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { readableError } from '../../../shared/errors'

type CopyIconButtonProps = {
    className: string
    copiedLabel: string
    label: string
    getText: () => string
}

/** 消息、代码和表格共用复制入口；表格在点击时读取当前 DOM 内容。 */
export function CopyIconButton({ className, copiedLabel, label, getText }: CopyIconButtonProps) {
    const [copied, setCopied] = useState(false)
    const [error, setError] = useState('')

    useEffect(() => {
        if (!copied) return
        const timer = window.setTimeout(() => setCopied(false), 1600)
        return () => window.clearTimeout(timer)
    }, [copied])

    const copy = async () => {
        setCopied(false)
        setError('')
        try {
            if (!navigator.clipboard?.writeText) throw new Error('当前环境无法访问剪贴板')
            await navigator.clipboard.writeText(getText())
            setCopied(true)
        } catch (reason) {
            setError(`复制失败：${readableError(reason)}`)
        }
    }

    return <>
        <button
            className={`${className} ${copied ? 'copied' : ''}`}
            type="button"
            title={error || (copied ? copiedLabel : label)}
            aria-label={copied ? copiedLabel : label}
            onClick={() => void copy()}
        >
            {copied ? <Check size={15} strokeWidth={2} aria-hidden="true" /> : <Copy size={15} strokeWidth={1.8} aria-hidden="true" />}
        </button>
        {error && <span className="message-copy-error" role="alert" title={error}>复制失败，请重试</span>}
    </>
}

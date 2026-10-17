import { useEffect, useRef, useState, type RefObject } from 'react'
import { ChevronDown, ShieldCheck } from 'lucide-react'
import { getProjectApprovalMode, setProjectApprovalMode, type ApprovalMode, type ProjectApprovalMode } from '../../api/projects'
import { readableError } from '../../shared/errors'

type ApprovalModeSelectProps = {
    projectId: string
    disabled: boolean
    pickerRef: RefObject<HTMLDivElement | null>
    open: boolean
    onToggle: () => void
    onClose: () => void
}

/** 项目级授权选择：复用输入区的触发器、菜单和浮层交互，后端提交成功才更新。 */
export function ApprovalModeSelect({ projectId, disabled, pickerRef, open, onToggle, onClose }: ApprovalModeSelectProps) {
    const [settings, setSettings] = useState<ProjectApprovalMode | null>(null)
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [error, setError] = useState('')
    const [retry, setRetry] = useState(0)
    const savingRef = useRef(false)

    useEffect(() => {
        let active = true
        setLoading(true)
        setError('')
        getProjectApprovalMode(projectId).then((value) => {
            if (active) setSettings(value)
        }).catch((err: unknown) => {
            if (active) setError(readableError(err))
        }).finally(() => {
            if (active) setLoading(false)
        })
        return () => { active = false }
    }, [projectId, retry])

    const save = async (mode: ApprovalMode | null) => {
        if (savingRef.current || loading || !settings) return
        onClose()
        savingRef.current = true
        setSaving(true)
        setError('')
        try {
            setSettings(await setProjectApprovalMode(projectId, mode))
        } catch (err) {
            setError(readableError(err))
        } finally {
            savingRef.current = false
            setSaving(false)
        }
    }

    const title = settings
        ? `项目默认动作：${settings.effectiveMode}${settings.projectMode === null ? '（继承全局）' : ''}。ask 询问用户；auto 由模型判断，危险拒绝、不确定询问；yolo 统一跳过审核，但不能覆盖 deny。具体规则优先于默认动作，已有人工等待不会静默执行。`
        : '正在读取项目级 Approval mode'
    const unavailable = disabled || loading || saving || !settings
    const label = saving ? '保存中…' : loading ? '加载中…' : !settings ? '授权不可用'
        : settings.effectiveMode
    const options: { mode: ApprovalMode | null; label: string; description: string }[] = [
        { mode: null, label: '继承全局', description: settings?.globalMode ?? '' },
        { mode: 'ask', label: 'ask', description: '询问' },
        { mode: 'auto', label: 'auto', description: '模型审核' },
        { mode: 'yolo', label: 'yolo', description: '跳过审核' },
    ]
    return <div className={`composer-select-picker composer-approval-mode ${unavailable ? 'is-disabled' : ''}`} ref={pickerRef}>
        <button
            className="composer-select-trigger composer-approval-trigger"
            type="button"
            title={title}
            aria-label={`Approval mode（项目默认）：${label}`}
            aria-busy={loading || saving}
            aria-expanded={open}
            aria-haspopup="menu"
            aria-controls="approval-mode-picker-menu"
            disabled={unavailable}
            onClick={onToggle}
        >
            <ShieldCheck size={15} strokeWidth={1.8} aria-hidden="true" />
            <span>{label}</span>
            <ChevronDown className={`composer-select-chevron ${open ? 'is-open' : ''}`} size={14} strokeWidth={2} aria-hidden="true" />
        </button>
        {open && settings && <div className="model-picker-menu" id="approval-mode-picker-menu" role="menu" aria-label="Approval mode 选择">
            {options.map((option) => <button
                className={`model-picker-option ${settings.projectMode === option.mode ? 'is-selected' : ''}`}
                type="button"
                role="menuitem"
                tabIndex={-1}
                key={option.mode ?? 'inherit'}
                aria-current={settings.projectMode === option.mode ? 'true' : undefined}
                onClick={() => void save(option.mode)}
            >
                <span className="model-picker-option-label">{option.label}</span>
                <span className="model-picker-option-provider">{option.description}</span>
            </button>)}
        </div>}
        {saving && <span role="status" className="composer-approval-status">保存中…</span>}
        {error && <span role="alert" className="composer-approval-error">
            <span>{error}</span>
            <button type="button" disabled={disabled || loading || saving} onClick={() => setRetry((value) => value + 1)}>重新读取</button>
        </span>}
    </div>
}

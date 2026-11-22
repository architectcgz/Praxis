import { useCallback, useRef, useState } from 'react'
import { selectSessionModel, type ApiErrorCode, type ModelOption, type ModelSelection } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

const emptySelection: ModelSelection = { providerId: '', modelId: '', reasoningLevel: '' }

type SelectionOverride = {
    sessionID: string
    selection: ModelSelection
    draftProviderID?: string
    pending: boolean
    failed: boolean
}

/** 模型支持思考强度时使用目录默认档位，否则留空。 */
function defaultReasoning(model: ModelOption): string {
    return model.reasoningLevels.length ? model.defaultReasoningLevel : ''
}

/** 同一会话内跨 Agent 使用相同选择；初始选择来自 SelectSession 快照，修改串行提交。 */
export function useAgentTaskInput(
    sessionID: string,
    models: ModelOption[],
    initialSelection: ModelSelection | null,
    onSelectionSaved: (selection: ModelSelection) => void,
) {
    const [input, setInput] = useState('')
    const [selectionError, setError] = useState('')
    const [selectionErrorCode, setErrorCode] = useState<ApiErrorCode | undefined>()
    const [override, setOverride] = useState<SelectionOverride | null>(null)
    const syncQueue = useRef<Promise<void>>(Promise.resolve())
    const generation = useRef(0)

    const activeOverride = override?.sessionID === sessionID ? override : null
    const selection = activeOverride?.selection ?? initialSelection ?? emptySelection
    const draftProviderID = activeOverride?.draftProviderID

    const applySelection = useCallback((next: ModelSelection) => {
        if (!sessionID) return
        const request = ++generation.current
        setError('')
        setErrorCode(undefined)
        setOverride({ sessionID, selection: next, pending: true, failed: false })
        // 请求携带固定会话身份；串行提交防止较早选择覆盖较新选择。
        const sync = syncQueue.current.catch(() => undefined).then(() => selectSessionModel(sessionID, next))
        syncQueue.current = sync
        void sync.then(() => {
            if (generation.current !== request) return
            onSelectionSaved(next)
            setOverride({ sessionID, selection: next, pending: false, failed: false })
        }, (error: unknown) => {
            if (generation.current !== request) return
            setOverride({ sessionID, selection: next, pending: false, failed: true })
            setError(readableError(error))
            setErrorCode(apiErrorCode(error))
        })
    }, [onSelectionSaved, sessionID])

    const selectModel = useCallback((providerId: string, modelId: string) => {
        if (!modelId) {
            if (sessionID && models.some((item) => item.providerId === providerId)) {
                // 切换 Provider 只更新界面草稿，选定完整模型前保留后端选择。
                setOverride((current) => ({
                    sessionID,
                    selection: current?.sessionID === sessionID ? current.selection : (initialSelection ?? emptySelection),
                    draftProviderID: providerId,
                    pending: false,
                    failed: false,
                }))
            }
            return
        }
        const model = models.find((item) => item.providerId === providerId && item.modelId === modelId)
        if (model) applySelection({ providerId, modelId, reasoningLevel: defaultReasoning(model) })
    }, [applySelection, initialSelection, models, sessionID])

    const selectReasoning = useCallback((level: string) => {
        const model = models.find((item) => item.providerId === selection.providerId && item.modelId === selection.modelId)
        if (model?.reasoningLevels.includes(level)) {
            applySelection({ providerId: selection.providerId, modelId: selection.modelId, reasoningLevel: level })
        }
    }, [applySelection, models, selection.modelId, selection.providerId])

    const clearSelectionError = useCallback(() => {
        setError('')
        setErrorCode(undefined)
    }, [])

    const modelSelectionPending = Boolean(sessionID && activeOverride?.pending)
    return {
        selectionError, selectionErrorCode, clearSelectionError, input, setInput, selectModel, selectReasoning,
        selectedProviderID: draftProviderID ?? selection.providerId,
        selectedModelID: draftProviderID === undefined ? selection.modelId : '',
        reasoning: draftProviderID === undefined ? selection.reasoningLevel : '',
        modelSelectionPending,
        modelSelectionReady: Boolean(sessionID && !modelSelectionPending && !activeOverride?.failed && selection.modelId !== ''),
    }
}

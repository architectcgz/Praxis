import { useCallback, useLayoutEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'
import { getSessionModelSelection, isBindingAvailable, selectSessionModel, type ApiErrorCode, type ModelOption, type ModelSelection } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

const emptySelection: ModelSelection = { providerId: '', modelId: '', reasoningLevel: '' }

type SelectionState = {
    sessionID: string
    selection: ModelSelection
    confirmedKey: string
    failed: boolean
}

/** 模型支持思考强度时使用目录默认档位，否则留空。 */
function defaultReasoning(model: ModelOption): string {
    return model.reasoningLevels.length ? model.defaultReasoningLevel : ''
}

/** 同一会话内跨 Agent 使用相同选择；切换会话只读取后端状态，不发布全局偏好。 */
export function useAgentTaskInput(
    sessionID: string,
    models: ModelOption[],
    setError: Dispatch<SetStateAction<string>>,
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>,
) {
    const [input, setInput] = useState('')
    const [state, setState] = useState<SelectionState>({ sessionID: '', selection: emptySelection, confirmedKey: '', failed: false })
    const syncQueue = useRef<Promise<void>>(Promise.resolve())
    const generation = useRef(0)
    const selection = state.sessionID === sessionID ? state.selection : emptySelection
    const selectionKey = JSON.stringify(selection)

    useLayoutEffect(() => {
        const request = ++generation.current
        if (sessionID && isBindingAvailable()) {
            setState({ sessionID, selection: emptySelection, confirmedKey: '', failed: false })
            // 读取排在此前的更新之后，快速切回会话时不会读到尚未提交的旧选择。
            const load = syncQueue.current.catch(() => undefined).then(() => getSessionModelSelection(sessionID))
            syncQueue.current = load.then(() => undefined, () => undefined)
            void load.then((value) => {
                if (generation.current !== request) return
                setState({ sessionID, selection: value, confirmedKey: JSON.stringify(value), failed: false })
            }, (error: unknown) => {
                if (generation.current !== request) return
                setState({ sessionID, selection: emptySelection, confirmedKey: JSON.stringify(emptySelection), failed: true })
                setError(readableError(error))
                setErrorCode(apiErrorCode(error))
            })
        }
        return () => { generation.current += 1 }
    }, [sessionID, setError, setErrorCode])

    const applySelection = useCallback((next: ModelSelection) => {
        if (!sessionID) return
        const request = ++generation.current
        setState({ sessionID, selection: next, confirmedKey: '', failed: false })
        // 请求携带固定会话身份；串行提交防止较早选择覆盖较新选择。
        const sync = syncQueue.current.catch(() => undefined).then(() => selectSessionModel(sessionID, {
            providerId: next.modelId ? next.providerId : '',
            modelId: next.modelId,
            reasoningLevel: next.reasoningLevel,
        }))
        syncQueue.current = sync
        void sync.then(() => {
            if (generation.current !== request) return
            setState({ sessionID, selection: next, confirmedKey: JSON.stringify(next), failed: false })
        }, (error: unknown) => {
            if (generation.current !== request) return
            setState({ sessionID, selection: next, confirmedKey: JSON.stringify(next), failed: true })
            setError(readableError(error))
            setErrorCode(apiErrorCode(error))
        })
    }, [sessionID, setError, setErrorCode])

    const selectModel = useCallback((providerId: string, modelId: string) => {
        if (!modelId) {
            if (models.some((item) => item.providerId === providerId)) {
                applySelection({ providerId, modelId: '', reasoningLevel: '' })
            }
            return
        }
        const model = models.find((item) => item.providerId === providerId && item.modelId === modelId)
        if (model) applySelection({ providerId, modelId, reasoningLevel: defaultReasoning(model) })
    }, [applySelection, models])

    const selectReasoning = useCallback((level: string) => {
        const model = models.find((item) => item.providerId === selection.providerId && item.modelId === selection.modelId)
        if (model?.reasoningLevels.includes(level)) {
            applySelection({ providerId: selection.providerId, modelId: selection.modelId, reasoningLevel: level })
        }
    }, [applySelection, models, selection.providerId, selection.modelId])

    const confirmed = state.sessionID === sessionID && state.confirmedKey === selectionKey
    return {
        input, setInput, selectModel, selectReasoning,
        selectedProviderID: selection.providerId,
        selectedModelID: selection.modelId,
        reasoning: selection.reasoningLevel,
        modelSelectionPending: Boolean(sessionID) && isBindingAvailable() && !confirmed,
        modelSelectionReady: Boolean(sessionID) && confirmed && !state.failed && selection.modelId !== '',
    }
}

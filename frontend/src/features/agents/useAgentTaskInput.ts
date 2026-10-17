import { useCallback, useMemo, useState } from 'react'
import type { AgentSnapshot, ModelOption } from '../../api'
import { readRememberedModelSelection, rememberModelSelection } from './modelPreference'

type ModelSelection = { catalogKey: string; providerId: string; modelId: string; reasoning: string }

/** 模型支持思考强度时使用目录默认档位，否则留空。 */
function defaultReasoning(model?: ModelOption): string {
    return model && model.reasoningLevels.length ? model.defaultReasoningLevel : ''
}

/** 草稿由操作入口管理；Provider/Model 选择会跨 Agent 与 Session 记忆，只有用户重新选择才改变。 */
export function useAgentTaskInput(agent: AgentSnapshot | null, models: ModelOption[]) {
    const [input, setInput] = useState('')
    const [selection, setSelection] = useState<ModelSelection | null>(null)
    const definitionId = agent?.definitionId || ''
    const catalogKey = JSON.stringify(models.map((model) => [
        model.providerId, model.modelId, model.reasoningLevels, model.defaultReasoningLevel,
        model.defaultProviderId, model.defaultModelId, model.assignedAgentDefinitions,
    ]))
    // 用户上次选定的模型优先；目录里已不存在时回退到按 Agent 定义计算的默认模型。
    const fallback = useMemo(() => {
        const remembered = readRememberedModelSelection()
        const rememberedModel = models.find((model) => model.providerId === remembered.providerId && model.modelId === remembered.modelId)
        if (rememberedModel) {
            return {
                providerId: rememberedModel.providerId,
                modelId: rememberedModel.modelId,
                reasoning: rememberedModel.reasoningLevels.includes(remembered.reasoning) ? remembered.reasoning : defaultReasoning(rememberedModel),
            }
        }
        const defaultModel = models.find((model) => model.assignedAgentDefinitions.includes(definitionId)) ||
            models.find((model) => model.providerId === model.defaultProviderId && model.modelId === model.defaultModelId) ||
            models.find((model) => model.providerId === model.defaultProviderId) || models[0]
        return {
            providerId: defaultModel?.providerId || '',
            modelId: defaultModel?.modelId || '',
            reasoning: defaultReasoning(defaultModel),
        }
    }, [definitionId, models])
    const current = selection?.catalogKey === catalogKey ? selection : { ...fallback, catalogKey }
    const selectedProviderID = current.providerId
    const selectedModelID = current.modelId
    const reasoning = current.reasoning

    const applySelection = useCallback((next: ModelSelection) => {
        setSelection(next)
        rememberModelSelection(next)
    }, [])

    const selectModel = useCallback((providerId: string, modelId: string) => {
        const model = models.find((item) => item.providerId === providerId && item.modelId === modelId)
        if (!model) return
        applySelection({ catalogKey, providerId, modelId, reasoning: defaultReasoning(model) })
    }, [applySelection, catalogKey, models])

    const selectReasoning = useCallback((level: string) => {
        const model = models.find((item) => item.providerId === selectedProviderID && item.modelId === selectedModelID)
        if (!model?.reasoningLevels.includes(level)) return
        applySelection({ catalogKey, providerId: selectedProviderID, modelId: selectedModelID, reasoning: level })
    }, [applySelection, catalogKey, models, selectedModelID, selectedProviderID])

    return { input, setInput, selectedProviderID, selectedModelID, reasoning, selectModel, selectReasoning }
}

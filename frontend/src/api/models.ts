import { getSessionBinding } from './bindings'

export type ModelOption = {
    providerId: string
    modelId: string
    label: string
    providerName: string
    reasoningLevels: string[]
    defaultReasoningLevel: string
}

export function reloadConfig(sessionID: string) {
    return getSessionBinding().ReloadConfig(sessionID)
}

export function normalizeModelOptions(value: readonly {
    providerId: string
    modelId: string
    label: string
    providerName: string
    reasoningLevels?: string[] | null
    defaultReasoningLevel?: string | null
}[] | null | undefined): ModelOption[] {
    if (!value) return []
    return value.map((model) => ({
        providerId: model.providerId,
        modelId: model.modelId,
        label: model.label,
        providerName: model.providerName,
        reasoningLevels: model.reasoningLevels ?? [],
        defaultReasoningLevel: model.defaultReasoningLevel ?? '',
    }))
}

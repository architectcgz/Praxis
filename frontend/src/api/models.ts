import { getSessionBinding } from './bindings'

export type ModelOption = {
    providerId: string
    modelId: string
    label: string
    providerName: string
    reasoningLevels: string[]
    defaultReasoningLevel: string
}

export function listModels(sessionID: string) {
    return getSessionBinding().ListModels(sessionID).then(normalizeModelCatalog)
}

export function reloadConfig(sessionID: string) {
    return getSessionBinding().ReloadConfig(sessionID)
}

function normalizeModelCatalog(value: unknown): ModelOption[] {
    if (!Array.isArray(value)) {
        throw new Error('Model 列表响应无效。')
    }
    return value.map((model) => {
        if (!isRecord(model)) {
            throw new Error('Model 列表响应无效。')
        }
        return {
            providerId: stringValue(model.providerId),
            modelId: stringValue(model.modelId),
            label: stringValue(model.label),
            providerName: stringValue(model.providerName),
            reasoningLevels: model.reasoningLevels == null ? [] : stringArray(model.reasoningLevels),
            defaultReasoningLevel: model.defaultReasoningLevel == null ? '' : stringValue(model.defaultReasoningLevel),
        }
    })
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null
}

function stringValue(value: unknown): string {
    if (typeof value !== 'string') {
        throw new Error('Model 列表响应无效。')
    }
    return value
}

function stringArray(value: unknown): string[] {
    if (!Array.isArray(value) || !value.every((item) => typeof item === 'string')) {
        throw new Error('Model 列表响应无效。')
    }
    return value
}

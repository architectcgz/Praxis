import type { ModelAPIFormat, ProviderConfigOption } from '../../../api'
import type { ModelDraft } from './modelConfigTypes'

export function createProviderID(): string {
    return `provider-${crypto.randomUUID()}`
}

export function providerDisplayName(provider: ProviderConfigOption): string {
    return provider.providerName || provider.baseURL
}

export function modelKey(providerId: string, modelId: string): string {
    return `${providerId}\u0000${modelId}`
}

export function modelDraftWithID(current: ModelDraft, modelId: string): ModelDraft {
    return {
        ...current,
        modelId,
        label: current.label && current.label !== current.modelId ? current.label : modelId,
    }
}

export function formatAPIFormat(format: ModelAPIFormat): string {
    switch (format) {
        case 'anthropic_messages':
            return 'Anthropic Messages'
        case 'openai_responses':
            return 'OpenAI Responses'
        case 'openai_chat_completions':
            return 'OpenAI Chat Completions'
    }
}

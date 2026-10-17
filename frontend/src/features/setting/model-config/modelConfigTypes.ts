import type { ModelAPIFormat, ModelConfigOption, ProviderConfigOption } from '../../../api'

export type ModelConfigPanelProps = {
    onRefresh: () => void
    onBack: () => void
}

export type Editor =
    | { kind: 'provider'; index: number | null }
    | { kind: 'model'; index: number | null; providerID: string }

export type ModelDraft = Omit<ModelConfigOption, 'apiFormat'> & {
    apiFormat: ModelAPIFormat | ''
}

export type ProviderDialogProps = {
    provider: ProviderConfigOption | null
    modelCount: number
    models: ModelConfigOption[]
    error: string
    saving: boolean
    onSave: (provider: ProviderConfigOption, key: string) => Promise<void>
    onDelete?: () => void
    onClose: () => void
}

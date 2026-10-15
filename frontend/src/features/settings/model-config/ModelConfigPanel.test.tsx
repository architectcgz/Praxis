import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'
import type { ModelConfigDocument } from '../../../api'
import { ModelConfigPanel } from './ModelConfigPanel'

const api = vi.hoisted(() => ({ getModelConfig: vi.fn(), saveModelConfig: vi.fn(), setProviderKey: vi.fn() }))
vi.mock('../../../api', () => api)
vi.mock('../../../shared/errors', () => ({ readableError: (error: Error) => error.message }))

beforeEach(() => vi.resetAllMocks())

it('Provider 已保存但 Key 写入失败时，重试只保留一个相同 ID 的 Provider', async () => {
    let config: ModelConfigDocument = { groups: [], providers: [], models: [], defaultProviderId: '' }
    api.getModelConfig.mockImplementation(async () => structuredClone(config))
    api.saveModelConfig.mockImplementation(async (next: ModelConfigDocument) => { config = structuredClone(next); return { saved: true } })
    api.setProviderKey.mockRejectedValueOnce(new Error('credential write failed')).mockResolvedValue(undefined)
    const onRefresh = vi.fn()
    render(<ModelConfigPanel onRefresh={onRefresh} onBack={() => {}} />)
    const add = screen.getByRole('button', { name: '添加 Provider' })
    await waitFor(() => expect((add as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(add)
    fireEvent.change(screen.getByLabelText('Provider 名称'), { target: { value: 'Test Provider' } })
    fireEvent.change(screen.getByLabelText('Base URL'), { target: { value: 'https://example.invalid' } })
    fireEvent.change(screen.getByLabelText('API Key'), { target: { value: 'test-key' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await screen.findAllByText('credential write failed')
    expect(config.providers).toHaveLength(1)
    const providerId = config.providers[0].id
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(onRefresh).toHaveBeenCalledOnce())
    expect(config.providers.map((provider) => provider.id)).toEqual([providerId])
    expect(api.setProviderKey).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('dialog')).toBeNull()
})

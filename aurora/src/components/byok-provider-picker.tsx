/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/*
Shared BYOK provider picker. A single source of truth for the mainstream
upstream providers offered in the "bring-your-own-key" dropdowns, used by both
the organization console BYOK tab and the personal BYOK page. Selecting a
provider yields its numeric channel type and its official base URL (still
editable at the call site).
*/
import { useTranslation } from 'react-i18next'

import { Combobox } from '@/components/ui/combobox'
import { CHANNEL_TYPES } from '@/features/channels/constants'
import { getChannelTypeIcon } from '@/features/channels/lib/channel-utils'
import { getLobeIcon } from '@/lib/lobe-icon'

// Mainstream providers offered in the BYOK dropdown. The base URLs are the
// providers' official endpoints, mirroring the backend ChannelBaseURLs table
// (constant/channel.go). Selecting one auto-fills the base URL (still editable)
// and sets the numeric channel type sent to the backend.
export const BYOK_PROVIDERS: { id: number; baseUrl: string }[] = [
  { id: 1, baseUrl: 'https://api.openai.com' }, // OpenAI
  { id: 14, baseUrl: 'https://api.anthropic.com' }, // Anthropic
  { id: 24, baseUrl: 'https://generativelanguage.googleapis.com' }, // Gemini
  { id: 43, baseUrl: 'https://api.deepseek.com' }, // DeepSeek
  { id: 25, baseUrl: 'https://api.moonshot.cn' }, // Moonshot
  { id: 42, baseUrl: 'https://api.mistral.ai' }, // Mistral
  { id: 20, baseUrl: 'https://openrouter.ai/api' }, // OpenRouter
  { id: 16, baseUrl: 'https://open.bigmodel.cn' }, // Zhipu
  { id: 17, baseUrl: 'https://dashscope.aliyuncs.com' }, // Ali / Qwen
  { id: 45, baseUrl: 'https://ark.cn-beijing.volces.com' }, // VolcEngine / Doubao
  { id: 48, baseUrl: 'https://api.x.ai' }, // xAI / Grok
  { id: 40, baseUrl: 'https://api.siliconflow.cn' }, // SiliconFlow
  { id: 27, baseUrl: 'https://api.perplexity.ai' }, // Perplexity
  { id: 34, baseUrl: 'https://api.cohere.ai' }, // Cohere
]

export const BYOK_BASE_URL: Record<number, string> = Object.fromEntries(
  BYOK_PROVIDERS.map((p) => [p.id, p.baseUrl])
)

type ByokProviderPickerProps = {
  /** Currently selected numeric channel type, as a string (empty when unset). */
  value: string
  /**
   * Called when a provider is picked. Receives the numeric channel type (as a
   * string, matching the Combobox value) and the provider's official base URL.
   */
  onSelect: (type: string, baseUrl: string) => void
}

export function ByokProviderPicker({ value, onSelect }: ByokProviderPickerProps) {
  const { t } = useTranslation()

  const providerOptions = BYOK_PROVIDERS.map((p) => ({
    value: String(p.id),
    label: (CHANNEL_TYPES as Record<number, string>)[p.id] ?? `#${p.id}`,
    icon: getLobeIcon(`${getChannelTypeIcon(p.id)}.Color`, 16),
  }))

  return (
    <Combobox
      options={providerOptions}
      value={value}
      onValueChange={(v) => v && onSelect(v, BYOK_BASE_URL[Number(v)] ?? '')}
      placeholder={t('Select a provider')}
      searchPlaceholder={t('Search providers...')}
      emptyText={t('No provider found.')}
    />
  )
}

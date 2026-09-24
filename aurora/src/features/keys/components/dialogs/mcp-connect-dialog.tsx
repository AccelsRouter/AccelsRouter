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
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { getOrgContext } from '@/features/organization-console/api'

function getServerAddress(): string {
  try {
    const raw = localStorage.getItem('status')
    if (raw) {
      const status = JSON.parse(raw)
      if (status.server_address) return status.server_address as string
    }
  } catch {
    /* empty */
  }
  return window.location.origin
}

type ClientId = 'claude-code' | 'cursor' | 'codex' | 'json'

const CLIENTS: { id: ClientId; label: string }[] = [
  { id: 'claude-code', label: 'Claude Code' },
  { id: 'cursor', label: 'Cursor' },
  { id: 'codex', label: 'Codex CLI' },
  { id: 'json', label: 'JSON' },
]

function buildSnippet(client: ClientId, url: string, key: string): string {
  const auth = `Bearer ${key}`
  switch (client) {
    case 'claude-code':
      return `claude mcp add --transport http --scope user accelsrouter ${url} --header "Authorization: ${auth}"`
    case 'cursor':
      return JSON.stringify(
        {
          mcpServers: {
            accelsrouter: { url, headers: { Authorization: auth } },
          },
        },
        null,
        2
      )
    case 'codex':
      return [
        '# ~/.codex/config.toml',
        '[mcp_servers.accelsrouter]',
        `url = "${url}"`,
        `http_headers = { "Authorization" = "${auth}" }`,
      ].join('\n')
    default:
      return JSON.stringify(
        {
          type: 'http',
          url,
          headers: { Authorization: auth },
        },
        null,
        2
      )
  }
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  tokenKey: string
}

export function McpConnectDialog(props: Props) {
  const { t } = useTranslation()
  const [client, setClient] = useState<ClientId>('claude-code')
  const { data: orgContext } = useQuery({
    queryKey: ['org-context'],
    queryFn: getOrgContext,
    enabled: props.open,
    staleTime: 60_000,
  })
  const isResellerAdmin = orgContext?.is_reseller_admin ?? false
  const url = `${getServerAddress().replace(/\/$/, '')}/mcp`
  const key = props.tokenKey.startsWith('sk-')
    ? props.tokenKey
    : `sk-${props.tokenKey}`
  const snippet = buildSnippet(client, url, key)

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Connect via MCP')}
      description={t(
        'Give your coding agent this gateway as an MCP server: it can browse models and prices, check credit, look up past requests and send test messages with this key.'
      )}
      contentClassName='sm:max-w-lg'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <Button variant='outline' onClick={() => props.onOpenChange(false)}>
          {t('Close')}
        </Button>
      }
    >
      <div className='space-y-1'>
        <div className='text-muted-foreground text-xs'>{t('Server URL')}</div>
        <div className='flex items-center gap-2'>
          <code className='bg-muted flex-1 truncate rounded px-2 py-1 font-mono text-sm'>
            {url}
          </code>
          <CopyButton value={url} size='sm' variant='outline' />
        </div>
      </div>

      <Tabs value={client} onValueChange={(v) => setClient(v as ClientId)}>
        <TabsList>
          {CLIENTS.map((c) => (
            <TabsTrigger key={c.id} value={c.id}>
              {c.label}
            </TabsTrigger>
          ))}
        </TabsList>
        {CLIENTS.map((c) => (
          <TabsContent key={c.id} value={c.id} className='space-y-2 pt-2'>
            <div className='relative'>
              <pre className='bg-muted max-h-64 overflow-auto rounded p-3 pr-12 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap'>
                {snippet}
              </pre>
              <CopyButton
                value={snippet}
                size='sm'
                variant='outline'
                className='absolute top-2 right-2'
              />
            </div>
          </TabsContent>
        ))}
      </Tabs>

      <p className='text-muted-foreground text-xs'>
        {t(
          'Only the send-message tool spends credit; every other tool is a free read-only lookup. The key is embedded in the snippet, so treat it like a password.'
        )}
      </p>
      {isResellerAdmin && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'As a distributor admin, your keys also unlock read-only reseller tools: customers, usage and profit, customer offers, call records and the wallet ledger. Keys bound to a customer workspace do not.'
          )}
        </p>
      )}
    </Dialog>
  )
}

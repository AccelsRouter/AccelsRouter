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
Admin live-concurrency view (fork): relay requests in progress right now on
every node, grouped by user, organization, model and channel, with the last
hour's peaks. Polls the read-only admin endpoint; nothing is stored.
*/
import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import {
  Activity,
  Gauge,
  Loader2,
  RefreshCw,
  Server,
  Users,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { StatCard } from '@/features/dashboard/components/ui/stat-card'
import { getInflight, type InflightCountRow } from './api'

const REFRESH_MS = 3000

function ageSeconds(startedAtMs: number, nowMs: number): string {
  const seconds = Math.max(0, Math.round((nowMs - startedAtMs) / 1000))
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

function CountTable(props: {
  rows: InflightCountRow[]
  keyLabel: string
  showName: boolean
  empty: string
}) {
  const { t } = useTranslation()
  if (props.rows.length === 0) {
    return (
      <p className='text-muted-foreground py-8 text-center text-sm'>
        {props.empty}
      </p>
    )
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{props.keyLabel}</TableHead>
          <TableHead className='text-right'>{t('In flight')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.rows.map((row) => (
          <TableRow key={row.key}>
            <TableCell>
              {props.showName && row.name ? (
                <span>
                  {row.name}{' '}
                  <span className='text-muted-foreground text-xs'>
                    #{row.key}
                  </span>
                </span>
              ) : (
                row.key
              )}
            </TableCell>
            <TableCell className='text-right tabular-nums'>
              {row.inflight}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

export function InflightMonitor() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('users')
  const query = useQuery({
    queryKey: ['admin-inflight'],
    queryFn: getInflight,
    refetchInterval: REFRESH_MS,
    placeholderData: keepPreviousData,
  })
  const data = query.data
  const nowMs = Date.now()
  const empty = t('No requests in flight right now.')

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Live Concurrency')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <span className='text-muted-foreground text-xs'>
          {t('Refreshes every 3 seconds')}
        </span>
        <Button
          variant='outline'
          size='sm'
          className='gap-1.5'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {query.isFetching ? (
            <Loader2 className='h-3.5 w-3.5 animate-spin' />
          ) : (
            <RefreshCw className='h-3.5 w-3.5' />
          )}
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-5'>
          <div className='grid grid-cols-2 gap-3 xl:grid-cols-4'>
            <StatCard
              title={t('In flight')}
              value={data?.total ?? 0}
              description={t('Requests')}
              icon={Activity}
              tone='teal'
              loading={query.isLoading}
            />
            <StatCard
              title={t('Peak (1h)')}
              value={data?.peak_total ?? 0}
              description={t(
                'Peaks are summed across nodes over the last hour, so a fleet-wide peak may read slightly high.'
              )}
              icon={Gauge}
              tone='gray'
              loading={query.isLoading}
            />
            <StatCard
              title={t('Users')}
              value={data?.users.filter((u) => u.inflight > 0).length ?? 0}
              description={t('In flight')}
              icon={Users}
              tone='gray'
              loading={query.isLoading}
            />
            <StatCard
              title={t('Nodes')}
              value={data?.nodes.length ?? 0}
              description={
                data?.nodes
                  .map((n) => `${n.node}: ${n.inflight}`)
                  .join(' · ') || '-'
              }
              icon={Server}
              tone='gray'
              loading={query.isLoading}
            />
          </div>
          {query.error ? (
            <p className='text-destructive text-sm'>
              {query.error instanceof Error
                ? query.error.message
                : t('Failed to load')}
            </p>
          ) : (
            <Tabs value={tab} onValueChange={setTab}>
              <TabsList>
                <TabsTrigger value='users'>{t('By user')}</TabsTrigger>
                <TabsTrigger value='orgs'>{t('By organization')}</TabsTrigger>
                <TabsTrigger value='models'>{t('By model')}</TabsTrigger>
                <TabsTrigger value='channels'>{t('By channel')}</TabsTrigger>
                <TabsTrigger value='entries'>{t('Live requests')}</TabsTrigger>
              </TabsList>
              <TabsContent value='users' className='pt-4'>
                {!data || data.users.length === 0 ? (
                  <p className='text-muted-foreground py-8 text-center text-sm'>
                    {empty}
                  </p>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('User')}</TableHead>
                        <TableHead>{t('Organization')}</TableHead>
                        <TableHead className='text-right'>
                          {t('In flight')}
                        </TableHead>
                        <TableHead className='text-right'>
                          {t('Peak (1h)')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {data.users.map((row) => (
                        <TableRow key={row.user_id}>
                          <TableCell>
                            {row.username || '-'}{' '}
                            <span className='text-muted-foreground text-xs'>
                              #{row.user_id}
                            </span>
                          </TableCell>
                          <TableCell>
                            {row.org_name ||
                              (row.org_id ? `#${row.org_id}` : '-')}
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {row.inflight}
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {row.peak}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </TabsContent>
              <TabsContent value='orgs' className='pt-4'>
                <CountTable
                  rows={data?.orgs ?? []}
                  keyLabel={t('Organization')}
                  showName
                  empty={empty}
                />
              </TabsContent>
              <TabsContent value='models' className='pt-4'>
                <CountTable
                  rows={data?.models ?? []}
                  keyLabel={t('Model')}
                  showName={false}
                  empty={empty}
                />
              </TabsContent>
              <TabsContent value='channels' className='pt-4'>
                <CountTable
                  rows={data?.channels ?? []}
                  keyLabel={t('Channel')}
                  showName
                  empty={empty}
                />
              </TabsContent>
              <TabsContent value='entries' className='pt-4'>
                {!data || data.entries.length === 0 ? (
                  <p className='text-muted-foreground py-8 text-center text-sm'>
                    {empty}
                  </p>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Age')}</TableHead>
                        <TableHead>{t('User')}</TableHead>
                        <TableHead>{t('Token')}</TableHead>
                        <TableHead>{t('Model')}</TableHead>
                        <TableHead>{t('Channel')}</TableHead>
                        <TableHead>{t('Path')}</TableHead>
                        <TableHead>{t('Node')}</TableHead>
                        <TableHead>{t('Request ID')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {data.entries.map((e) => (
                        <TableRow
                          key={e.request_id || `${e.node}-${e.started_at}`}
                        >
                          <TableCell className='tabular-nums'>
                            {ageSeconds(e.started_at, nowMs)}
                          </TableCell>
                          <TableCell>{e.username || `#${e.user_id}`}</TableCell>
                          <TableCell className='text-muted-foreground'>
                            {e.token_name || '-'}
                          </TableCell>
                          <TableCell>{e.model || '-'}</TableCell>
                          <TableCell>
                            {e.channel_id > 0 ? `#${e.channel_id}` : '-'}
                          </TableCell>
                          <TableCell className='text-muted-foreground text-xs'>
                            {e.path}
                          </TableCell>
                          <TableCell className='text-muted-foreground text-xs'>
                            {e.node}
                          </TableCell>
                          <TableCell className='font-mono text-xs'>
                            {e.request_id || '-'}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </TabsContent>
            </Tabs>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

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
Model identity verification for one channel × model: run the canary probe,
read the checks against the baseline, capture this channel as the baseline,
and copy the evidence (exact request and response) to hand to the upstream.
*/
import { useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, Loader2, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  getUpstreamIdentityResult,
  probeUpstreamIdentity,
  setUpstreamIdentityBaseline,
  type IdentityCheck,
  type IdentityVerdict,
  type ModelFingerprint,
  type UpstreamIdentityResult,
} from './api'
import { fmtAgo } from './shared'

export const IDENTITY_QUERY_KEY = ['admin-upstream-identity']

export const VERDICT_META: Record<
  IdentityVerdict,
  { key: string; className: string }
> = {
  verified: {
    key: 'Verified',
    className: 'border-emerald-500/40 text-emerald-600 dark:text-emerald-400',
  },
  suspect: {
    key: 'Suspect',
    className: 'border-red-500/40 text-red-600 dark:text-red-400',
  },
  unverified: {
    key: 'Unverified',
    className: 'border-muted-foreground/40 text-muted-foreground',
  },
  error: {
    key: 'Probe error',
    className: 'border-amber-500/40 text-amber-600 dark:text-amber-400',
  },
}

export function parseChecks(json: string | undefined): IdentityCheck[] {
  if (!json) return []
  try {
    const v = JSON.parse(json)
    return Array.isArray(v) ? (v as IdentityCheck[]) : []
  } catch {
    return []
  }
}

const CHECK_LABEL: Record<string, string> = {
  response_model: 'Echoed model name',
  response_id: 'Response id shape',
  prompt_tokens: 'Canary input tokens (tokenizer fingerprint)',
  usage_source: 'Usage source',
  probe: 'Probe',
}

export function IdentityDialog(props: {
  open: boolean
  onOpenChange: (o: boolean) => void
  channelId: number
  channelName: string
  model: string
  summary?: UpstreamIdentityResult
  baseline?: ModelFingerprint
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const detail = useQuery({
    queryKey: ['admin-upstream-identity-result', props.channelId, props.model],
    queryFn: () => getUpstreamIdentityResult(props.channelId, props.model),
    enabled: props.open && !!props.summary,
    retry: false,
  })
  const result = detail.data ?? props.summary
  const checks = useMemo(() => parseChecks(result?.checks), [result?.checks])
  const evidence = useMemo(() => {
    if (!detail.data?.evidence) return null
    try {
      return JSON.parse(detail.data.evidence) as {
        request: string
        response: string
        upstream_model: string
        error?: string
      }
    } catch {
      return null
    }
  }, [detail.data?.evidence])

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: IDENTITY_QUERY_KEY })
    void queryClient.invalidateQueries({
      queryKey: [
        'admin-upstream-identity-result',
        props.channelId,
        props.model,
      ],
    })
  }
  const probe = useMutation({
    mutationFn: () => probeUpstreamIdentity(props.channelId, props.model),
    onSuccess: (r) => {
      toast.success(
        t('Verification finished: {{verdict}}', {
          verdict: t(VERDICT_META[r.verdict]?.key ?? r.verdict),
        })
      )
      refresh()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })
  const baseline = useMutation({
    mutationFn: () => setUpstreamIdentityBaseline(props.channelId, props.model),
    onSuccess: (fp) => {
      toast.success(
        t('Baseline captured: {{n}} canary tokens', { n: fp.prompt_tokens })
      )
      refresh()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })
  const busy = probe.isPending || baseline.isPending

  const copyEvidence = async () => {
    const payload = {
      channel_id: props.channelId,
      channel: props.channelName,
      model: props.model,
      verdict: result?.verdict,
      checked_at: result?.created_time,
      checks,
      baseline: props.baseline ?? null,
      evidence,
    }
    try {
      await navigator.clipboard.writeText(JSON.stringify(payload, null, 2))
      toast.success(t('Evidence copied'))
    } catch {
      toast.error(t('Failed to copy to clipboard'))
    }
  }

  const meta = result ? VERDICT_META[result.verdict] : undefined
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex max-h-[85vh] flex-col gap-4 overflow-hidden sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <ShieldCheck className='h-4 w-4' />
            {t('Model identity')}
            <span className='text-muted-foreground font-mono text-sm font-normal'>
              {props.channelName} · {props.model}
            </span>
          </DialogTitle>
          <DialogDescription>
            {t(
              'A fixed canary is sent through this channel and compared with the baseline captured from a channel you trust. Keep one direct official channel per model as the baseline source.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-wrap items-center gap-3 text-sm'>
          {meta ? (
            <Badge variant='outline' className={cn('text-xs', meta.className)}>
              {t(meta.key)}
            </Badge>
          ) : (
            <Badge variant='outline' className='text-muted-foreground text-xs'>
              {t('Not verified yet')}
            </Badge>
          )}
          {result && (
            <span className='text-muted-foreground text-xs'>
              {fmtAgo(result.created_time, t)} · {result.latency_ms} ms
            </span>
          )}
          <span className='text-muted-foreground ml-auto text-xs'>
            {props.baseline
              ? t('Baseline from channel #{{id}}: {{n}} tokens', {
                  id: props.baseline.source_channel_id,
                  n: props.baseline.prompt_tokens,
                })
              : t('No baseline for this model')}
          </span>
        </div>

        {checks.length > 0 && (
          <div className='border-border/60 min-w-0 overflow-x-auto rounded-md border'>
            <table className='w-full table-fixed text-sm'>
              <colgroup>
                <col className='w-[38%]' />
                <col className='w-[20%]' />
                <col className='w-[30%]' />
                <col className='w-[12%]' />
              </colgroup>
              <thead className='bg-muted/40 text-muted-foreground text-xs'>
                <tr>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Check')}
                  </th>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Expected')}
                  </th>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Actual')}
                  </th>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Result')}
                  </th>
                </tr>
              </thead>
              <tbody className='divide-border/60 divide-y'>
                {checks.map((c) => (
                  <tr key={c.name}>
                    <td className='px-3 py-2'>
                      <div>{t(CHECK_LABEL[c.name] ?? c.name)}</div>
                      {c.note && (
                        <div className='text-muted-foreground text-xs'>
                          {c.note}
                        </div>
                      )}
                    </td>
                    <td className='px-3 py-2 font-mono text-xs break-all'>
                      {c.expected}
                    </td>
                    <td className='px-3 py-2 font-mono text-xs break-all'>
                      {c.actual}
                    </td>
                    <td className='px-3 py-2'>
                      <Badge
                        variant='outline'
                        className={cn(
                          'text-[10px]',
                          c.status === 'pass' &&
                            'border-emerald-500/40 text-emerald-600 dark:text-emerald-400',
                          c.status === 'fail' &&
                            'border-red-500/40 text-red-600 dark:text-red-400',
                          c.status === 'unknown' &&
                            'border-muted-foreground/40 text-muted-foreground'
                        )}
                      >
                        {c.status === 'pass'
                          ? t('pass')
                          : c.status === 'fail'
                            ? t('fail')
                            : t('unknown')}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {evidence && (
          <details className='min-w-0 text-xs'>
            <summary className='text-muted-foreground cursor-pointer'>
              {t('Evidence (exact request and response)')}
            </summary>
            <pre className='bg-muted/40 mt-2 max-h-56 max-w-full overflow-auto rounded-md p-2 font-mono break-all whitespace-pre-wrap'>
              {evidence.request}
              {'\n\n--- response ---\n'}
              {evidence.error ? evidence.error : evidence.response}
            </pre>
          </details>
        )}

        <DialogFooter className='gap-2 sm:justify-between'>
          <div className='flex gap-2'>
            <Button
              variant='outline'
              size='sm'
              onClick={() => void copyEvidence()}
              disabled={!result}
            >
              <Copy className='h-3.5 w-3.5' />
              {t('Copy evidence')}
            </Button>
          </div>
          <div className='flex gap-2'>
            <Button
              variant='outline'
              size='sm'
              disabled={busy}
              onClick={() => baseline.mutate()}
              title={t(
                "Capture this channel's answer as the trusted baseline for the model"
              )}
            >
              {baseline.isPending && (
                <Loader2 className='h-3.5 w-3.5 animate-spin' />
              )}
              {t('Set as baseline')}
            </Button>
            <Button size='sm' disabled={busy} onClick={() => probe.mutate()}>
              {probe.isPending && (
                <Loader2 className='h-3.5 w-3.5 animate-spin' />
              )}
              {t('Verify now')}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

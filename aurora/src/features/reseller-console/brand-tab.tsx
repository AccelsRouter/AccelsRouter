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
Reseller white-label brand. What the reseller sets here (name + logo) is what
its downstream customers see in place of the platform brand across their console.
*/
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  getResellerBrand,
  setResellerBrand,
} from '@/features/organization-console/api'
import { Field } from '@/features/organization-console/shared'

export function BrandTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [logo, setLogo] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['reseller-brand'],
    queryFn: getResellerBrand,
    staleTime: 60_000,
  })

  useEffect(() => {
    if (data) {
      setName(data.brand_name)
      setLogo(data.brand_logo)
    }
  }, [data])

  const mutation = useMutation({
    mutationFn: () =>
      setResellerBrand({ brand_name: name.trim(), brand_logo: logo.trim() }),
    onSuccess: () => {
      toast.success(t('Brand updated'))
      queryClient.invalidateQueries({ queryKey: ['reseller-brand'] })
      // Any customer console open elsewhere re-reads its brand from context.
      queryClient.invalidateQueries({ queryKey: ['org-context'] })
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const logoValid =
    logo.trim() === '' ||
    /^(https?:\/\/|data:image\/)/.test(logo.trim())

  if (isLoading) {
    return (
      <div className='flex h-40 items-center justify-center'>
        <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
      </div>
    )
  }

  return (
    <div className='flex max-w-xl flex-col gap-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Your customers see this name and logo instead of the platform brand. Leave empty to fall back to the platform brand.'
        )}
      </p>
      <Field label={t('Brand name')}>
        <Input
          value={name}
          maxLength={64}
          onChange={(e) => setName(e.target.value)}
          placeholder={t('e.g. Acme AI')}
        />
      </Field>
      <Field label={t('Brand logo URL')}>
        <Input
          value={logo}
          onChange={(e) => setLogo(e.target.value)}
          placeholder='https://…/logo.png'
        />
        {!logoValid && (
          <span className='text-destructive text-xs'>
            {t('Logo must be an http(s) link or image data.')}
          </span>
        )}
      </Field>

      <div className='flex items-center gap-3'>
        <span className='text-muted-foreground text-xs'>{t('Preview')}</span>
        <div className='border-border/60 flex items-center gap-2 rounded-lg border p-2'>
          {logo.trim() && logoValid ? (
            <img
              src={logo.trim()}
              alt={name || t('Logo')}
              className='h-6 w-6 rounded object-cover'
            />
          ) : (
            <div className='bg-muted h-6 w-6 rounded' />
          )}
          <span className='text-sm font-semibold'>
            {name.trim() || t('Your brand')}
          </span>
        </div>
      </div>

      <div>
        <Button
          onClick={() => mutation.mutate()}
          disabled={!logoValid || mutation.isPending}
          className='gap-1.5'
        >
          {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
          {t('Save')}
        </Button>
      </div>
    </div>
  )
}

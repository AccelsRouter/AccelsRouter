import { Inbox } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { useMyNotifications } from './use-my-notifications'

/** The user's own notifications, newest first, as a tab of the bell. */
export function MyNotifications() {
  const { t } = useTranslation()
  const notifications = useMyNotifications()

  if (notifications.items.length === 0) {
    return (
      <Empty className='min-h-48 border-0 p-4'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <Inbox />
          </EmptyMedia>
          <EmptyTitle>
            {notifications.loading ? t('Loading...') : t('No messages yet')}
          </EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <ScrollArea className='h-[min(52vh,28rem)] pr-3'>
      <div className='flex flex-col'>
        {notifications.items.map((item, index) => (
          <div key={item.id}>
            <div className='flex items-start gap-3 py-3'>
              <span
                className={cn(
                  'mt-1.5 inline-block size-2 shrink-0 rounded-full',
                  item.read_at === 0 ? 'bg-primary' : 'bg-muted'
                )}
                aria-hidden='true'
              />
              <div className='flex min-w-0 flex-1 flex-col gap-1'>
                <p className='text-sm break-words'>
                  {t(item.message.key, item.message.params ?? {})}
                </p>
                <p className='text-muted-foreground text-xs'>
                  {formatDateTimeObject(new Date(item.created_at * 1000))}
                </p>
              </div>
            </div>
            {index < notifications.items.length - 1 ? <Separator /> : null}
          </div>
        ))}
      </div>
    </ScrollArea>
  )
}

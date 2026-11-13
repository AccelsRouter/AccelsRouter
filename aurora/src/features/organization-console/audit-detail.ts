/*
What an organization audit row did, as a sentence. The server stores each row
as an action code, a target ("org:6", "user:12") and a short detail in one of
a few machine shapes; this reads all of them, and falls back to the stored
detail for an action it does not know.
*/
import type { TFunction } from 'i18next'
import { formatQuotaWithCurrency } from '@/lib/currency'
import type { OrgAuditLog } from './types'

// "quota=100 delta=100 trade=T1" and its like.
function pairs(detail: string): Record<string, string> {
  const values: Record<string, string> = {}
  for (const part of detail.split(' ')) {
    const at = part.indexOf('=')
    if (at > 0) values[part.slice(0, at)] = part.slice(at + 1)
  }
  return values
}

// Go's print of a map, "map[name:acme status:active]", the shape update rows
// were written in. The keys are the setting names a row can hold; a row cut
// off at the column's length has lost its closing bracket.
const GO_MAP_KEYS =
  /(?:^| )(allowed_models|monthly_budget|name|price_group|remark|role|status|updated_time|wholesale_ratios|wholesale_ratio):/g

function goMapFields(detail: string): Record<string, string> | null {
  if (!detail.startsWith('map[')) return null
  const body = detail.slice(4).replace(/\]$/, '')
  const marks = [...body.matchAll(GO_MAP_KEYS)]
  const values: Record<string, string> = {}
  marks.forEach((mark, index) => {
    const start = (mark.index ?? 0) + mark[0].length
    const end = marks[index + 1]?.index ?? body.length
    values[mark[1]] = body.slice(start, end)
  })
  return values
}

function jsonFields(detail: string): Record<string, unknown> | null {
  if (!detail.startsWith('{')) return null
  try {
    const parsed: unknown = JSON.parse(detail)
    return parsed && typeof parsed === 'object'
      ? (parsed as Record<string, unknown>)
      : null
  } catch {
    return null
  }
}

function statusText(value: unknown, t: TFunction): string {
  if (value === 'active') return t('Active')
  if (value === 'suspended') return t('Suspended')
  return String(value)
}

function roleText(value: unknown, t: TFunction): string {
  if (value === 'owner') return t('Owner')
  if (value === 'admin') return t('Admin')
  if (value === 'member') return t('Member')
  return String(value)
}

// Every setting an update row names, each as "name set to acme".
function settingChanges(
  fields: Record<string, unknown>,
  t: TFunction
): string[] {
  const changes: string[] = []
  for (const [key, value] of Object.entries(fields)) {
    switch (key) {
      case 'name':
        changes.push(t('name set to {{value}}', { value }))
        break
      case 'price_group':
        changes.push(t('price group set to {{value}}', { value }))
        break
      case 'status':
        changes.push(
          t('status set to {{value}}', { value: statusText(value, t) })
        )
        break
      case 'role':
        changes.push(t('role set to {{value}}', { value: roleText(value, t) }))
        break
      case 'remark':
        changes.push(
          value
            ? t('remark set to “{{value}}”', { value })
            : t('remark cleared')
        )
        break
      case 'remark_changed':
        changes.push(t('remark changed'))
        break
      case 'monthly_budget':
        changes.push(
          Number(value) > 0
            ? t('monthly budget set to {{amount}}', {
                amount: formatQuotaWithCurrency(Number(value)),
              })
            : t('monthly budget lifted')
        )
        break
      case 'wholesale_ratio':
        changes.push(t('wholesale ratio set to {{value}}', { value }))
        break
      case 'wholesale_ratios': {
        if (typeof value === 'number') {
          changes.push(t('{{n}} per-model wholesale ratios set', { n: value }))
          break
        }
        const ratios = Object.entries(value as Record<string, number>)
        changes.push(
          ratios.length > 0
            ? t('per-model wholesale ratios set to {{value}}', {
                value: ratios
                  .map(([model, ratio]) => `${model} ${ratio}`)
                  .join(', '),
              })
            : t('per-model wholesale ratios cleared')
        )
        break
      }
      case 'models_added':
        // The names, or how many there are when the row kept only the count.
        changes.push(
          Array.isArray(value)
            ? t('callable models added: {{models}}', {
                models: value.join(', '),
              })
            : t('{{n}} callable models added', { n: Number(value) })
        )
        break
      case 'models_removed':
        // The names, or how many there are when the row kept only the count.
        changes.push(
          Array.isArray(value)
            ? t('callable models removed: {{models}}', {
                models: value.join(', '),
              })
            : t('{{n}} callable models removed', { n: Number(value) })
        )
        break
      case 'models_limited':
        // The names, or how many there are when the row kept only the count.
        changes.push(
          Array.isArray(value)
            ? t('callable models limited to: {{models}}', {
                models: value.join(', '),
              })
            : t('callable models limited to {{n}} models', { n: Number(value) })
        )
        break
      case 'models_unrestricted':
        changes.push(t('model restriction lifted'))
        break
    }
  }
  return changes
}

// The settings of an older update row, which named every setting the form
// held, changed or not. Empty ones said nothing and are left out.
function savedSettings(
  fields: Record<string, string>
): Record<string, unknown> {
  const settings: Record<string, unknown> = {}
  for (const [key, text] of Object.entries(fields)) {
    if (key === 'updated_time' || text === '') continue
    if (key === 'allowed_models') {
      // A JSON list, possibly cut off: take the names it still holds.
      settings.models_limited = [...text.matchAll(/"([^"]+)"/g)].map(
        (match) => match[1]
      )
    } else if (key === 'wholesale_ratios') {
      try {
        settings[key] = JSON.parse(text) as Record<string, number>
      } catch {
        // Cut off: the row no longer holds the ratios.
      }
    } else {
      settings[key] = text
    }
  }
  return settings
}

function updateText(
  detail: string,
  heading: (changes: string) => string,
  t: TFunction
): string | null {
  const older = goMapFields(detail)
  const fields = older ? savedSettings(older) : jsonFields(detail)
  if (!fields) return null
  const changes = settingChanges(fields, t)
  return changes.length > 0 ? heading(changes.join('; ')) : null
}

export function auditDetailText(entry: OrgAuditLog, t: TFunction): string {
  const detail = entry.detail || ''
  const values = pairs(detail)
  const target =
    entry.target_name || `#${entry.target.slice(entry.target.indexOf(':') + 1)}`
  const amount = (quota: string | undefined) =>
    formatQuotaWithCurrency(Math.abs(Number(quota) || 0))
  const withOrder = (text: string) =>
    values.trade
      ? t('{{text}} (order no. {{trade}})', { text, trade: values.trade })
      : text

  switch (entry.action) {
    case 'org.create':
      return t('Created the organization {{name}}', {
        name: detail.replace(/ \([a-z_]+\)$/, ''),
      })
    case 'org.update':
      if (detail === '{}') {
        return t('Saved the organization settings without changes')
      }
      // An older row lists every setting the form held, changed or not.
      return (
        updateText(
          detail,
          (changes) =>
            detail.startsWith('map[')
              ? t('Saved the organization settings: {{changes}}', { changes })
              : t('Updated the organization settings: {{changes}}', {
                  changes,
                }),
          t
        ) ?? detail
      )
    case 'org.add':
      return withOrder(
        t('Added {{amount}} to the wallet', { amount: amount(values.quota) })
      )
    case 'org.reduce':
      return withOrder(
        t('Deducted {{amount}} from the wallet', {
          amount: amount(values.delta),
        })
      )
    case 'org.set':
      return withOrder(
        t('Set the wallet balance to {{amount}}', {
          amount: amount(values.quota),
        })
      )
    case 'account.attach':
      return t('Added member {{target}} as {{role}}', {
        target,
        role: roleText(values.role, t),
      })
    case 'account.update':
      return (
        updateText(
          detail,
          (changes) =>
            t('Updated member {{target}}: {{changes}}', { target, changes }),
          t
        ) ?? detail
      )
    case 'account.detach':
      return t('Removed member {{target}}', { target })
    case 'account.leave':
      return t('Member {{target}} left the organization', { target })
    case 'reseller_admin.revoke':
      return t('Revoked the distributor admin role of {{target}}', { target })
    case 'reseller_admin.status':
      return t('Set distributor admin {{target}} to {{status}}', {
        target,
        status: statusText(detail, t),
      })
    case 'credit.allocate':
      return t('Allocated {{amount}} to customer {{target}}', {
        amount: amount(values.quota),
        target,
      })
    case 'credit.revoke':
      return t('Took back {{amount}} from customer {{target}}', {
        amount: amount(values.quota),
        target,
      })
    case 'wallet.purchase':
      return t(
        'Bought {{credit}} of wallet credit with {{cost}} from the personal balance',
        { credit: amount(values.credit), cost: amount(values.cost) }
      )
    case 'sso.domain.add':
      return t(
        'Added the SSO domain {{domain}} for sign-in through {{provider}}',
        { domain: entry.target, provider: values.provider }
      )
    case 'sso.domain.delete':
      return t('Removed an SSO domain')
    case 'customer.create':
      return t(
        'Created customer {{name}} with a starting balance of {{amount}}',
        {
          name: detail.replace(/ quota=\d+$/, ''),
          amount: amount(values.quota),
        }
      )
    case 'customer.provision':
      return t('Opened an account for {{email}} to own customer {{target}}', {
        email: detail,
        target,
      })
    case 'customer.invite':
      return t('Invited {{email}} to own customer {{target}}', {
        email: detail,
        target,
      })
    case 'customer.provision.resend':
    case 'invitation.provision.resend':
      return t('Sent the activation email to {{email}} again', {
        email: detail,
      })
    case 'customer.invite.revoke':
      return t('Revoked an invitation of customer {{target}}', { target })
    case 'invitation.provision':
      return t('Opened an account for {{email}}', { email: detail })
    case 'customer.pricing':
      return t('Set {{n}} discounts for customer {{target}}', {
        n: Number(values.count),
        target,
      })
    case 'customer.models':
      return Number(values.count) > 0
        ? t('Limited customer {{target}} to {{n}} models', {
            target,
            n: Number(values.count),
          })
        : t('Lifted the model restriction of customer {{target}}', { target })
    case 'customer.offer':
      return Number(values.models) > 0
        ? t(
            'Saved the offer of customer {{target}}: {{models}} models, {{discounts}} discounts',
            {
              target,
              models: Number(values.models),
              discounts: Number(values.discounts),
            }
          )
        : t(
            'Saved the offer of customer {{target}}: every model, {{discounts}} discounts',
            { target, discounts: Number(values.discounts) }
          )
    case 'customer.remove':
      return t('Unbound customer {{target}} and disabled {{n}} of its keys', {
        target,
        n: Number(values.keys_disabled),
      })
    case 'reseller.brand':
      return detail
        ? t('Set the brand name to {{name}}', { name: detail })
        : t('Cleared the brand name')
    case 'workspace.create':
      return t('Created the workspace {{name}}', { name: detail })
    case 'workspace.delete':
      return t('Deleted the workspace {{name}}', { name: detail })
    case 'workspace.update':
      return (
        updateText(
          detail,
          (changes) =>
            t('Updated workspace {{target}}: {{changes}}', { target, changes }),
          t
        ) ?? detail
      )
    case 'workspace.token.bind':
      return t('Bound a key to workspace {{target}}', { target })
    case 'workspace.key.create':
    case 'reseller.key.create':
    case 'member.key.create':
      return t('Created the key {{name}} in workspace {{target}}', {
        name: detail.replace(/^token:\d+ /, ''),
        target,
      })
    case 'reseller.key.update':
    case 'member.key.update':
      return t('Updated the key {{name}}', { name: detail })
    case 'reseller.key.delete':
    case 'member.key.delete':
      return t('Deleted a key')
    case 'byok.create':
      return t('Added the BYOK channel {{name}}', { name: detail })
    case 'byok.delete':
      return t('Removed the BYOK channel {{name}}', { name: detail })
  }
  return detail || '-'
}

import type { TFunction } from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { auditDetailText } from './audit-detail'
import type { OrgAuditLog } from './types'

// The key with its values filled in, as the English locale renders it.
const t = ((key: string, values?: Record<string, unknown>) =>
  key.replace(/\{\{(\w+)\}\}/g, (_, name: string) =>
    String(values?.[name])
  )) as unknown as TFunction

function row(
  action: string,
  target: string,
  detail: string,
  targetName?: string
): OrgAuditLog {
  return {
    id: 1,
    org_id: 6,
    actor_user_id: 1,
    action,
    target,
    detail,
    created_time: 0,
    target_name: targetName,
  }
}

describe('auditDetailText', () => {
  test('an update names what changed', () => {
    assert.equal(
      auditDetailText(
        row(
          'org.update',
          'org:6',
          '{"models_added":["wan3.0-video"],"status":"suspended"}',
          'wahaha'
        ),
        t
      ),
      'Updated the organization settings: callable models added: wan3.0-video; status set to Suspended'
    )
    assert.equal(
      auditDetailText(row('org.update', 'org:6', '{"models_removed":60}'), t),
      'Updated the organization settings: 60 callable models removed'
    )
    assert.equal(
      auditDetailText(row('org.update', 'org:6', '{}'), t),
      'Saved the organization settings without changes'
    )
  })

  test('an older update row reads its Go map, cut off or not', () => {
    assert.equal(
      auditDetailText(
        row(
          'org.update',
          'org:6',
          'map[allowed_models:["glm-5-2-260617","qwen3.8-flash"] name:wahaha price_group:default remark: status:active updated_time:1793869786 wholesale_ratios:]'
        ),
        t
      ),
      'Saved the organization settings: callable models limited to: glm-5-2-260617, qwen3.8-flash; name set to wahaha; price group set to default; status set to Active'
    )
    assert.equal(
      auditDetailText(
        row(
          'org.update',
          'org:6',
          'map[allowed_models:["glm-5-2-260617","qwen3.8-fl'
        ),
        t
      ),
      'Saved the organization settings: callable models limited to: glm-5-2-260617'
    )
    assert.equal(
      auditDetailText(
        row(
          'workspace.update',
          'workspace:3',
          'map[monthly_budget:0 status:suspended]',
          'prod'
        ),
        t
      ),
      'Updated workspace prod: monthly budget lifted; status set to Suspended'
    )
  })

  test('other actions read their target and detail', () => {
    assert.equal(
      auditDetailText(
        row('customer.offer', 'org:9', 'models=0 discounts=2', 'acme'),
        t
      ),
      'Saved the offer of customer acme: every model, 2 discounts'
    )
    assert.equal(
      auditDetailText(row('account.detach', 'user:12', ''), t),
      'Removed member #12'
    )
    assert.equal(
      auditDetailText(row('reseller.key.update', 'token:13', 'test'), t),
      'Updated the key test'
    )
    assert.equal(
      auditDetailText(
        row('workspace.key.create', 'workspace:3', 'token:13 ci key', 'prod'),
        t
      ),
      'Created the key ci key in workspace prod'
    )
    assert.equal(
      auditDetailText(row('some.new.action', 'x:1', 'raw detail'), t),
      'raw detail'
    )
  })
})

import { describe, expect, it } from 'vitest'
import { filterNavigationGroups } from './navigation'

const groups = [
  {
    id: 'main',
    items: [{ id: 'overview' }, { id: 'sms', capability: 'sms_read' }],
  },
  {
    id: 'voice',
    items: [{ id: 'calls', capability: 'call_monitor' }],
  },
]

describe('filterNavigationGroups', () => {
  it('keeps unguarded items and removes empty groups', () => {
    expect(filterNavigationGroups(groups, {})).toEqual([
      {
        id: 'main',
        items: [{ id: 'overview' }],
      },
    ])
  })

  it('keeps items that match the capability snapshot', () => {
    expect(filterNavigationGroups(groups, { sms_read: 'available' })).toEqual([
      {
        id: 'main',
        items: [{ id: 'overview' }, { id: 'sms', capability: 'sms_read' }],
      },
    ])
  })

  it('can render the complete navigation while capability data is unavailable', () => {
    expect(groups).toHaveLength(2)
  })
})

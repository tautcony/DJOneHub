export interface CapabilityNavigationItem {
  capability?: string
}

export interface CapabilityNavigationGroup<TItem extends CapabilityNavigationItem> {
  items: TItem[]
}

export function filterNavigationGroups<
  TItem extends CapabilityNavigationItem,
  TGroup extends CapabilityNavigationGroup<TItem>,
>(groups: TGroup[], capabilities: Record<string, unknown>): TGroup[] {
  return groups
    .map((group) => ({
      ...group,
      items: group.items.filter(
        (item) => !item.capability || Object.prototype.hasOwnProperty.call(capabilities, item.capability),
      ),
    }))
    .filter((group) => group.items.length > 0)
}

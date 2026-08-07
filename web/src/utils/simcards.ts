import type { SimCard } from '../types'

export function simCardLabel(
  iccid: string,
  cards: SimCard[],
  formatICCID: (value: string) => string = (value) => value,
): string {
  const name = cards.find((card) => card.iccid === iccid)?.name?.trim()
  const displayedICCID = formatICCID(iccid)
  return name ? `${name} (${displayedICCID})` : displayedICCID
}

export function simICCIDs(cards: SimCard[], observedICCIDs: Array<string | undefined>): string[] {
  return [
    ...new Set(
      [...cards.map((card) => card.iccid), ...observedICCIDs]
        .map((iccid) => iccid?.trim() || '')
        .filter(Boolean),
    ),
  ]
}

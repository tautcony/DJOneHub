import type { SimProfile } from '../types'

export function simProfileLabel(
  iccid: string,
  profiles: SimProfile[],
  formatICCID: (value: string) => string = (value) => value,
): string {
  const name = profiles.find((profile) => profile.iccid === iccid)?.name?.trim()
  const displayedICCID = formatICCID(iccid)
  return name ? `${name} (${displayedICCID})` : displayedICCID
}

export function simProfileICCIDs(
  profiles: SimProfile[],
  observedICCIDs: Array<string | undefined>,
): string[] {
  return [
    ...new Set(
      [...profiles.map((profile) => profile.iccid), ...observedICCIDs]
        .map((value) => value?.trim())
        .filter((value): value is string => !!value),
    ),
  ]
}

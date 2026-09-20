export function isValidMk2ShowingId(value: string): boolean {
  const match = /^([0-9]+)-([1-9][0-9]*)$/.exec(value)
  return (
    value.length <= 128 - 'mk2-showing-'.length &&
    match !== null &&
    match[0] === value &&
    /[1-9]/.test(match[1]!)
  )
}

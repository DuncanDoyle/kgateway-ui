// region Helpers
/** Path of an install page, or of `rest` under it. The ID is encoded, never parsed (D-016). */
export function installPath(id: string, rest = ""): string {
  const base = `/i/${encodeURIComponent(id)}`;
  return rest ? `${base}/${rest}` : base;
}

/** An :id route parameter as the install ID; a malformed escape stays as it is. */
export function decodeInstallId(raw: string): string {
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

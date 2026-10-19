/** Error thrown for non-2xx API responses. */
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

/** Fetches JSON from the backend API. */
export async function apiFetch<T>(path: string): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { headers: { Accept: "application/json" } });
  } catch (e) {
    throw new ApiError(0, `Cannot reach the UI backend: ${(e as Error).message}`);
  }
  if (!res.ok) {
    let msg = res.statusText;
    try {
      msg = ((await res.json()) as { error?: string }).error ?? msg;
    } catch {
      // body was not JSON
    }
    throw new ApiError(res.status, msg);
  }
  try {
    return (await res.json()) as T;
  } catch {
    throw new ApiError(res.status, "The UI backend sent an unreadable response");
  }
}

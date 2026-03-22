export type APIError = {
  error: {
    code: string
    message: string
  }
}

async function decodeError(response: Response): Promise<APIError> {
  try {
    return (await response.json()) as APIError
  } catch {
    return {
      error: {
        code: 'unexpected_response',
        message: `请求失败，状态码 ${response.status}`,
      },
    }
  }
}

export async function requestJSON<T>(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(input, {
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  })

  if (!response.ok) {
    const payload = await decodeError(response)
    throw new Error(payload.error.message)
  }

  return (await response.json()) as T
}

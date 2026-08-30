type RequestOptions = Omit<RequestInit, "body"> & {
  body?: unknown;
};

export class ApiClient {
  async bootstrap(): Promise<void> {
    await this.request<void>("/api/session/bootstrap", { method: "POST" });
    await this.request<{ ok: boolean }>("/api/health");
  }

  async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const response = await fetch(path, {
      ...options,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      headers: {
        "Content-Type": "application/json",
        ...(options.headers || {}),
      },
    });
    const text = await response.text();
    if (!response.ok) throw new Error(errorMessage(text, response.statusText));
    if (!text) return undefined as T;
    return JSON.parse(text) as T;
  }

  async download(path: string, body: unknown, fallbackFilename: string): Promise<void> {
    const response = await fetch(path, {
      method: "POST",
      body: JSON.stringify(body),
      headers: { "Content-Type": "application/json" },
    });
    if (!response.ok) {
      const text = await response.text();
      throw new Error(errorMessage(text, response.statusText));
    }
    saveBlob(responseFilename(response.headers.get("Content-Disposition"), fallbackFilename), await response.blob());
  }

  async downloadGet(path: string, fallbackFilename: string): Promise<void> {
    const response = await fetch(path);
    if (!response.ok) {
      const text = await response.text();
      throw new Error(errorMessage(text, response.statusText));
    }
    saveBlob(responseFilename(response.headers.get("Content-Disposition"), fallbackFilename), await response.blob());
  }
}

function errorMessage(text: string, fallback: string): string {
  if (!text) return fallback;
  try {
    return (JSON.parse(text) as { error?: string }).error || fallback;
  } catch {
    return text;
  }
}

function responseFilename(contentDisposition: string | null, fallback: string): string {
  const encoded = contentDisposition?.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  if (encoded) return decodeURIComponent(encoded);
  return contentDisposition?.match(/filename="([^"]+)"/i)?.[1] || fallback;
}

function saveBlob(filename: string, content: Blob): void {
  const url = URL.createObjectURL(content);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

export const api = new ApiClient();

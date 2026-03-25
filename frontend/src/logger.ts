type ClientLogLevel = "info" | "warn" | "error";

type ClientLogPayload = {
  category: string;
  message: string;
  level?: ClientLogLevel;
  details?: Record<string, unknown>;
};

let fetchLoggingInstalled = false;
let navigationLoggingInstalled = false;
let rawFetch: typeof window.fetch = window.fetch.bind(window);

function clientTimestamp(): string {
  return new Date().toISOString();
}

function normalizeDetails(details?: Record<string, unknown>): Record<string, unknown> {
  if (!details) {
    return {};
  }

  const normalizedEntries = Object.entries(details).map(([key, value]) => {
    if (value instanceof Error) {
      return [key, { message: value.message, stack: value.stack }];
    }
    return [key, value];
  });

  return Object.fromEntries(normalizedEntries);
}

export function logClientEvent(payload: ClientLogPayload): void {
  const level = payload.level ?? "info";
  const timestamp = clientTimestamp();
  const details = normalizeDetails(payload.details);
  const line = `[${timestamp}] [${payload.category.toUpperCase()}] ${payload.message}`;

  if (level === "error") {
    console.error(line, details);
  } else if (level === "warn") {
    console.warn(line, details);
  } else {
    console.log(line, details);
  }

  void rawFetch("/api/logs/event", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    credentials: "same-origin",
    body: JSON.stringify({
      category: payload.category,
      details,
      level,
      message: payload.message,
      path: window.location.pathname,
      timestamp,
    }),
  }).catch(() => {
    // Ignore recursive transport failures; the console line already exists.
  });
}

export function installGlobalClientLogging(): void {
  installFetchLogging();
  installNavigationLogging();
  installWindowErrorLogging();
}

function installFetchLogging(): void {
  if (fetchLoggingInstalled) {
    return;
  }

  fetchLoggingInstalled = true;
  const originalFetch = window.fetch.bind(window);
  rawFetch = originalFetch;

  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const method = init?.method || (input instanceof Request ? input.method : "GET");
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    const startedAt = performance.now();

    if (url.includes("/api/logs/event")) {
      return originalFetch(input, init);
    }

    const suppressSuccessLog = shouldSuppressSuccessfulNetworkLog(url, method);

    if (!suppressSuccessLog) {
      logClientEvent({
        category: "network",
        message: "request started",
        details: {
          method,
          url,
        },
      });
    }

    try {
      const response = await originalFetch(input, init);
      if (!suppressSuccessLog || !response.ok) {
        logClientEvent({
          category: "network",
          message: "request completed",
          details: {
            durationMs: Math.round(performance.now() - startedAt),
            method,
            ok: response.ok,
            status: response.status,
            url,
          },
          level: response.ok ? "info" : "warn",
        });
      }
      return response;
    } catch (error) {
      logClientEvent({
        category: "network",
        message: "request failed",
        details: {
          durationMs: Math.round(performance.now() - startedAt),
          error: error instanceof Error ? error.message : String(error),
          method,
          url,
        },
        level: "error",
      });
      throw error;
    }
  };
}

function shouldSuppressSuccessfulNetworkLog(url: string, method: string): boolean {
  return method.toUpperCase() === "GET" && url.includes("/api/session");
}

function installNavigationLogging(): void {
  if (navigationLoggingInstalled) {
    return;
  }

  navigationLoggingInstalled = true;

  const logNavigation = (kind: string) => {
    logClientEvent({
      category: "navigation",
      message: "route changed",
      details: {
        kind,
        path: window.location.pathname,
        search: window.location.search,
      },
    });
  };

  const originalPushState = window.history.pushState.bind(window.history);
  window.history.pushState = function pushState(data: unknown, unused: string, url?: string | URL | null): void {
    originalPushState(data, unused, url);
    logNavigation("pushState");
  };

  const originalReplaceState = window.history.replaceState.bind(window.history);
  window.history.replaceState = function replaceState(data: unknown, unused: string, url?: string | URL | null): void {
    originalReplaceState(data, unused, url);
    logNavigation("replaceState");
  };

  window.addEventListener("popstate", () => logNavigation("popstate"));
  window.addEventListener("hashchange", () => logNavigation("hashchange"));
  logNavigation("initial");
}

function installWindowErrorLogging(): void {
  window.addEventListener("error", (event) => {
    logClientEvent({
      category: "window",
      message: "unhandled error",
      details: {
        filename: event.filename,
        line: event.lineno,
        message: event.message,
      },
      level: "error",
    });
  });

  window.addEventListener("unhandledrejection", (event) => {
    logClientEvent({
      category: "window",
      message: "unhandled promise rejection",
      details: {
        reason: event.reason instanceof Error ? event.reason.message : String(event.reason),
      },
      level: "error",
    });
  });
}

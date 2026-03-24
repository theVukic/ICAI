type SessionInfo = {
  tokenConfigured: boolean;
  username: string;
  fullName: string;
  authenticated?: boolean;
  appUsername?: string;
  appRole?: string;
  profileName?: string;
  availableTokens?: string[];
};

export type ListingLimits = {
  tokenConfigured: boolean;
  unlimited: boolean;
  segment: string;
  remaining: number;
  totalLimit: number;
  totalListings: number;
  checkedAt?: string;
  segments: Array<{
    segment: string;
    limit: number;
    listings: number;
    remaining: number;
  }>;
};

let cachedSession: SessionInfo | null = null;
let sessionPromise: Promise<SessionInfo> | null = null;

export async function loadSessionInfo(): Promise<SessionInfo> {
  if (cachedSession) {
    return cachedSession;
  }

  if (sessionPromise) {
    return sessionPromise;
  }

  sessionPromise = fetch("/api/session")
    .then(async (response) => {
      if (!response.ok) {
        throw new Error("Provjera session-a nije uspjela.");
      }
      const data = (await response.json()) as SessionInfo;
      cachedSession = data;
      return data;
    })
    .finally(() => {
      sessionPromise = null;
    });

  return sessionPromise;
}

export function resetSessionInfoCache(): void {
  cachedSession = null;
  sessionPromise = null;
}

export async function loginApp(
  username: string,
  password: string,
  rememberMe: boolean,
): Promise<void> {
  const response = await fetch("/api/login", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      username,
      password,
      remember_me: rememberMe,
    }),
  });

  const data = (await response.json()) as { error?: string; message?: string };
  if (!response.ok) {
    throw new Error(data.message || data.error || "Login nije uspio.");
  }

  resetSessionInfoCache();
}

export async function logoutApp(): Promise<void> {
  await fetch("/api/logout", {
    method: "POST",
  });

  resetSessionInfoCache();
}

export async function switchOlxToken(name: string): Promise<SessionInfo> {
  const response = await fetch("/api/olx/token", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ name }),
  });

  const data = (await response.json()) as SessionInfo | { error?: string };
  if (!response.ok) {
    throw new Error("error" in data && data.error ? data.error : "Promjena OLX tokena nije uspjela.");
  }

  const nextSession = data as SessionInfo;
  cachedSession = {
    ...(cachedSession || {}),
    ...nextSession,
    authenticated:
      nextSession.authenticated ??
      cachedSession?.authenticated,
    appUsername:
      nextSession.appUsername ??
      cachedSession?.appUsername,
    appRole:
      nextSession.appRole ??
      cachedSession?.appRole,
  };
  sessionPromise = null;
  return cachedSession;
}

export async function fetchListingLimits(): Promise<ListingLimits> {
  const response = await fetch("/api/listing-limits");
  const data = (await response.json()) as ListingLimits | { error?: string };
  if (!response.ok) {
    throw new Error("error" in data && data.error ? data.error : "Ucitavanje limita nije uspelo.");
  }

  return data as ListingLimits;
}

export function renderSessionBadge(session: SessionInfo | null): string {
  if (!session?.tokenConfigured || !session.username) {
    return "";
  }

  return `
    <div class="session-badge">
      <span>OLX</span>
      <strong>${escapeHtml(session.username)}</strong>
    </div>
  `;
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

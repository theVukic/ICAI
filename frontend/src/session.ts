type SessionInfo = {
  tokenConfigured: boolean;
  username: string;
  fullName: string;
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
      const data = (await response.json()) as SessionInfo;
      cachedSession = data;
      return data;
    })
    .finally(() => {
      sessionPromise = null;
    });

  return sessionPromise;
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

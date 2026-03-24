type PublishSessionStats = {
  successCount: number;
  failedCount: number;
};

const state: PublishSessionStats = {
  successCount: 0,
  failedCount: 0,
};

export function recordPublishSessionOutcome(successCount: number, failedCount: number): void {
  state.successCount += Math.max(0, successCount);
  state.failedCount += Math.max(0, failedCount);
}

export function getPublishSessionStats(): PublishSessionStats {
  return {
    successCount: state.successCount,
    failedCount: state.failedCount,
  };
}

export function renderPublishSessionBadge(): string {
  if (state.successCount <= 0 && state.failedCount <= 0) {
    return "";
  }

  return `
    <div class="publish-session-badge" aria-live="polite">
      ${state.successCount > 0 ? `<span class="publish-session-badge-success">${state.successCount}</span>` : ""}
      ${state.failedCount > 0 ? `<span class="publish-session-badge-failed">${state.failedCount}</span>` : ""}
    </div>
  `;
}

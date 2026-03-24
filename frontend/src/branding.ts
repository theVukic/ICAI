export const APP_TITLE = "ICAI";

export function renderAppBrand(extraContent = ""): string {
  return `
    <div class="logo-wrap">
      <div class="logo-mark">
        <img src="/app-logo.png" alt="${APP_TITLE} logo" />
      </div>
      <strong>${APP_TITLE}</strong>
      ${extraContent}
    </div>
  `;
}

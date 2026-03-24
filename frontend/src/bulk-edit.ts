import { navigateWithFade } from "./transition";
import { renderPublishSessionBadge } from "./publish-session";
import { renderSessionBadge } from "./session";
import { renderAppBrand } from "./branding";

const SELECTED_TOWKODS_STORAGE_KEY = "icaiapp:selected-towkods";

const backIcon = `
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path fill-rule="evenodd" clip-rule="evenodd" d="M14.7071 6.29289C15.0976 6.68342 15.0976 7.31658 14.7071 7.70711L10.4142 12l4.2929 4.2929C15.0976 16.6834 15.0976 17.3166 14.7071 17.7071 14.3166 18.0976 13.6834 18.0976 13.2929 17.7071l-5.00001-5C7.90237 12.3166 7.90237 11.6834 8.29289 11.2929L13.2929 6.29289c.390500000000001-.39052 1.0237-.39052 1.4142.0z"/>
  </svg>
`;

const saveIcon = `
  <svg viewBox="144 144 512 512" aria-hidden="true">
    <g>
      <path d="m273.49 208.7h66.102v125H273.49z"/>
      <path d="m573.11 609.23c6.6797 0 13.086-2.6523 17.809-7.3789 4.7266-4.7227 7.3789-11.129 7.3789-17.812v-314.53c0.011719-6.6836-2.6328-13.102-7.3555-17.836l-35.621-35.621 0.003907 0.003906c-4.7227-4.7891-11.16-7.4922-17.887-7.5078h-20.152v140.26c0 4.0078-1.5938 7.8516-4.4258 10.688-2.8359 2.832-6.6797 4.4258-10.688 4.4258h-243.79c-4.0117 0-7.8555-1.5938-10.688-4.4258-2.8359-2.8359-4.4297-6.6797-4.4297-10.688v-140.26h-20.152c-6.6797 0-13.086 2.6523-17.812 7.3789-4.7227 4.7227-7.375 11.129-7.375 17.812v350.15c0 6.6797 2.6523 13.086 7.375 17.812 4.7266 4.7227 11.133 7.375 17.812 7.375zM243.16 425.19c0.28516-4.2656 2.2422-8.25 5.4531-11.082 3.207-2.832 7.4023-4.2812 11.676-4.0312h275.43c4.2695-0.25 8.4648 1.1992 11.676 4.0312 3.207 2.832 5.168 6.8164 5.4531 11.082v146.66h-309.69z"/>
      <path d="m369.87 208.7h117.39v125H369.87z"/>
      <path d="m506.5 510.59H289.51c-5.3984 0-10.387 2.8789-13.09 7.5547-2.6992 4.6758-2.6992 10.438 0 15.117 2.7031 4.6758 7.6914 7.5547 13.09 7.5547H506.5c5.3984 0 10.391-2.8789 13.09-7.5547 2.6992-4.6797 2.6992-10.441 0-15.117s-7.6914-7.5547-13.09-7.5547z"/>
      <path d="m506.5 445.34H289.51c-5.3984 0-10.387 2.8828-13.09 7.5586-2.6992 4.6758-2.6992 10.438 0 15.113 2.7031 4.6758 7.6914 7.5586 13.09 7.5586H506.5c5.3984 0 10.391-2.8828 13.09-7.5586s2.6992-10.438 0-15.113c-2.6992-4.6758-7.6914-7.5586-13.09-7.5586z"/>
    </g>
  </svg>
`;

type CatalogImage = {
  position: number;
  url: string;
};

type CatalogStatus = {
  code: string;
  name: string;
};

type CatalogItem = {
  towKod: string;
  index: string;
  nameB2B: string;
  brandName: string;
  images: CatalogImage[];
  technicalDescription: string;
  status: CatalogStatus;
  retailPrice: number;
  currencyCode: string;
  sourceFileName?: string;
};

type UploadedCatalog = {
  id: string;
  fileName: string;
  savedPath: string;
  uploadedAt: string;
  totalItems: number;
};

type CatalogState = {
  catalogs: UploadedCatalog[];
  items: CatalogItem[];
  tempDir: string;
  totalFiles: number;
  totalItems: number;
  lastUpdated: string | null;
};

type ErrorResponse = {
  error: string;
};

const state: {
  items: CatalogItem[];
  isLoading: boolean;
  isSaving: boolean;
  message: string;
  tone: "muted" | "danger";
  sessionUsername: string;
  tokenConfigured: boolean;
} = {
  items: [],
  isLoading: false,
  isSaving: false,
  message: "",
  tone: "muted",
  sessionUsername: "",
  tokenConfigured: false,
};

export function mountBulkEditPage(root: HTMLDivElement): void {
  if (!state.isLoading && state.items.length === 0) {
    void loadBulkEditItems(root);
  }

  renderBulkEditPage(root);
}

export function setBulkEditPageSession(sessionUsername: string, tokenConfigured: boolean): void {
  state.sessionUsername = sessionUsername;
  state.tokenConfigured = tokenConfigured;
}

async function loadBulkEditItems(root: HTMLDivElement): Promise<void> {
  state.isLoading = true;
  state.message = "";
  renderBulkEditPage(root);

  try {
    const selectedTowKods = loadStoredSelectedTowKods();
    const response = await fetch("/api/catalog");
    const data = (await response.json()) as CatalogState | ErrorResponse;
    if (!response.ok) {
      throw new Error("error" in data ? data.error : "Ucitavanje nije uspelo.");
    }

    const catalogState = data as CatalogState;
    const selectedSet = new Set(selectedTowKods);
    state.items = catalogState.items.filter((item) => selectedSet.has(item.towKod));

    if (state.items.length === 0) {
      state.message = "Nema odabranih artikala za promjenu.";
      state.tone = "danger";
    }
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Ucitavanje nije uspelo.";
    state.tone = "danger";
  } finally {
    state.isLoading = false;
    renderBulkEditPage(root);
  }
}

function renderBulkEditPage(root: HTMLDivElement): void {
  const hasItems = state.items.length > 0;

  root.innerHTML = `
    <div class="shell edit-shell">
      <header class="topbar edit-topbar">
        ${renderAppBrand(
          `
            ${renderSessionBadge(
              state.tokenConfigured
                ? { tokenConfigured: true, username: state.sessionUsername, fullName: "" }
                : null,
            )}
            ${renderPublishSessionBadge()}
          `,
        )}
      </header>

      <main class="edit-page bulk-edit-page">
        <section class="bulk-edit-layout">
          <aside class="bulk-preview-panel">
            <div class="edit-copy">
              <p class="edit-eyebrow">ODABRANI ARTIKLI</p>
              <div class="bulk-preview-summary">
                <strong>${hasItems ? `${state.items.length}` : "0"}</strong>
                <span>${hasItems ? "artikala za promjenu" : "nema odabranih artikala"}</span>
              </div>
            </div>
            <div class="bulk-preview-list">
              ${
                state.isLoading
                  ? `<div class="edit-state">Ucitavam odabrane artikle...</div>`
                  : hasItems
                    ? state.items
                        .map(
                          (item) => `
                            <article class="bulk-preview-card">
                              <div class="bulk-preview-media">
                                ${
                                  item.images[0]?.url
                                    ? `<img src="${item.images[0].url}" alt="${escapeHtml(item.nameB2B || "Artikal")}" loading="lazy" draggable="false" />`
                                    : `<div class="bulk-preview-placeholder">Nema slike</div>`
                                }
                              </div>
                              <div class="bulk-preview-content">
                                <div class="bulk-preview-head">
                                  <strong>${escapeHtml(item.nameB2B || "Artikal bez naziva")}</strong>
                                </div>
                                <div class="bulk-preview-meta">
                                  <span>${escapeHtml(item.index || "-")}</span>
                                  <strong>${escapeHtml(formatPrice(item.retailPrice, item.currencyCode))}</strong>
                                </div>
                              </div>
                            </article>
                          `,
                        )
                        .join("")
                    : `<div class="edit-state ${state.tone === "danger" ? "danger" : ""}">${escapeHtml(state.message || "Nema odabranih artikala.")}</div>`
              }
            </div>
          </aside>

          <section class="bulk-form-panel">
            <article class="edit-hero-card bulk-form-card">
              <div class="edit-hero-body bulk-form-body">
                ${
                  state.isLoading
                    ? `<div class="edit-state">Ucitavam formu...</div>`
                    : hasItems
                      ? `
                        <form class="edit-form edit-form-large" id="bulkEditForm">
                          <div class="bulk-form-intro">
                            <p class="edit-eyebrow">GRUPNA PROMJENA</p>
                            <p class="edit-subtitle">Popuni samo polja koja zelis promijeniti. Prazna polja ostaju neizmijenjena.</p>
                          </div>

                          <label class="edit-field">
                            <span class="edit-label-tight">Ime</span>
                            <input id="bulkEditName" name="name" type="text" value="" placeholder="Ostavi prazno ako ne mijenjas" />
                          </label>

                          <label class="edit-field">
                            <span>Cijena</span>
                            <input id="bulkEditPrice" name="price" type="number" min="0" step="0.01" value="" placeholder="Ostavi prazno ako ne mijenjas" />
                          </label>

                          <label class="edit-field edit-field-fill">
                            <span class="edit-label-tight">Deskripcija</span>
                            <textarea id="bulkEditDescription" name="description" rows="10" placeholder="Ostavi prazno ako ne mijenjas"></textarea>
                          </label>

                          ${
                            state.message
                              ? `<div class="edit-state ${state.tone === "danger" ? "danger" : ""}">${escapeHtml(state.message)}</div>`
                              : ""
                          }

                          <div class="edit-card-footer">
                            <button class="ghost-button edit-action-button edit-back-button" type="button" id="bulkBackButton">
                              <span class="edit-action-icon">${backIcon}</span>
                              <span>Nazad</span>
                            </button>
                            <button class="save-button" type="submit" ${state.isSaving ? "disabled" : ""}>
                              <span class="edit-action-icon">${saveIcon}</span>
                              <span>${state.isSaving ? "Cuvam..." : "Sacuvaj"}</span>
                            </button>
                          </div>
                        </form>
                      `
                      : `
                        <div class="edit-state ${state.tone === "danger" ? "danger" : ""}">
                          ${escapeHtml(state.message || "Nema odabranih artikala za promjenu.")}
                        </div>
                        <div class="edit-card-footer">
                          <button class="ghost-button edit-action-button edit-back-button" type="button" id="bulkBackButton">
                            <span class="edit-action-icon">${backIcon}</span>
                            <span>Nazad</span>
                          </button>
                        </div>
                      `
                }
              </div>
            </article>
          </section>
        </section>
      </main>
    </div>
  `;

  bindBulkEditEvents(root);
}

function bindBulkEditEvents(root: HTMLDivElement): void {
  const backButton = document.querySelector<HTMLButtonElement>("#bulkBackButton");
  const form = document.querySelector<HTMLFormElement>("#bulkEditForm");

  backButton?.addEventListener("click", () => {
    navigateWithFade(root, "/");
  });

  form?.addEventListener("submit", async (event) => {
    event.preventDefault();

    const nameInput = document.querySelector<HTMLInputElement>("#bulkEditName");
    const priceInput = document.querySelector<HTMLInputElement>("#bulkEditPrice");
    const descriptionInput =
      document.querySelector<HTMLTextAreaElement>("#bulkEditDescription");

    if (!nameInput || !priceInput || !descriptionInput) {
      return;
    }

    const payload: Record<string, unknown> = {
      towKods: state.items.map((item) => item.towKod),
    };

    if (nameInput.value.trim() !== "") {
      payload.name = nameInput.value.trim();
    }
    if (priceInput.value.trim() !== "") {
      payload.price = Number(priceInput.value);
    }
    if (descriptionInput.value.trim() !== "") {
      payload.description = descriptionInput.value.trim();
    }

    if (Object.keys(payload).length === 1) {
      state.message = "Popuni barem jedno polje za izmjenu.";
      state.tone = "danger";
      renderBulkEditPage(root);
      return;
    }

    state.isSaving = true;
    state.message = "";
    renderBulkEditPage(root);

    try {
      const response = await fetch("/api/catalog/bulk-edit", {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(payload),
      });

      const data = (await response.json()) as CatalogItem[] | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "Cuvanje nije uspelo.");
      }

      state.isSaving = false;
      navigateWithFade(root, "/");
    } catch (error) {
      state.message =
        error instanceof Error ? error.message : "Cuvanje nije uspelo.";
      state.tone = "danger";
      state.isSaving = false;
      renderBulkEditPage(root);
    }
  });
}

function loadStoredSelectedTowKods(): string[] {
  const rawValue = window.localStorage.getItem(SELECTED_TOWKODS_STORAGE_KEY);
  if (!rawValue) {
    return [];
  }

  try {
    const parsed = JSON.parse(rawValue);
    return Array.isArray(parsed)
      ? parsed.filter((value): value is string => typeof value === "string" && value.length > 0)
      : [];
  } catch {
    return [];
  }
}

function formatPrice(value: number, currencyCode: string): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "Na upit";
  }

  const normalizedValue = Number.isInteger(value)
    ? String(value)
    : value.toFixed(2).replace(/\.?0+$/, "");
  return `${normalizedValue} ${currencyCode === "BAM" ? "KM" : currencyCode || "EUR"}`;
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

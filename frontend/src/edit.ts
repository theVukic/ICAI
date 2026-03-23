import { navigateWithFade } from "./transition";

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

type ErrorResponse = {
  error: string;
};

const state: {
  item: CatalogItem | null;
  isLoading: boolean;
  isSaving: boolean;
  message: string;
  tone: "muted" | "danger";
  activeTowKod: string;
  activeImageIndex: number;
} = {
  item: null,
  isLoading: false,
  isSaving: false,
  message: "",
  tone: "muted",
  activeTowKod: "",
  activeImageIndex: 0,
};

export function mountEditPage(root: HTMLDivElement, towKod: string): void {
  if (state.activeTowKod !== towKod || (!state.item && !state.isLoading)) {
    state.activeTowKod = towKod;
    state.activeImageIndex = 0;
    void loadEditItem(root, towKod);
  }

  renderEditPage(root, towKod);
}

async function loadEditItem(root: HTMLDivElement, towKod: string): Promise<void> {
  state.isLoading = true;
  state.item = null;
  state.message = "";
  renderEditPage(root, towKod);

  try {
    const response = await fetch(`/api/catalog/item/${encodeURIComponent(towKod)}`);
    const data = (await response.json()) as CatalogItem | ErrorResponse;

    if (!response.ok) {
      throw new Error("error" in data ? data.error : "Ucitavanje nije uspelo.");
    }

    state.item = data as CatalogItem;
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Ucitavanje nije uspelo.";
    state.tone = "danger";
  } finally {
    state.isLoading = false;
    renderEditPage(root, towKod);
  }
}

function renderEditPage(root: HTMLDivElement, towKod: string): void {
  const item = state.item;
  const imageCount = item?.images?.length ?? 0;
  const safeImageIndex =
    imageCount > 0 ? Math.min(state.activeImageIndex, imageCount - 1) : 0;
  const image = item?.images?.[safeImageIndex]?.url ?? "";
  const priceValue =
    item && Number.isFinite(item.retailPrice) ? String(item.retailPrice) : "0";

  root.innerHTML = `
    <div class="shell edit-shell">
      <header class="topbar edit-topbar">
        <div class="logo-wrap">
          <div class="logo-mark">IC</div>
          <strong>ICAI</strong>
        </div>
      </header>

      <main class="edit-page">
        <section class="edit-card-shell">
          <article class="edit-hero-card">
            <div class="edit-hero-media">
              ${
                image
                  ? `<img id="editImage" src="${image}" alt="${escapeHtml(item?.nameB2B || towKod)}" loading="lazy" draggable="false" />`
                  : `<div class="product-placeholder" id="editImagePlaceholder">Nema slike</div>`
              }
              ${
                imageCount > 1
                  ? `
                    <button class="image-arrow left is-visible" data-edit-image-nav="prev" type="button" aria-label="Prethodna slika">
                      <svg viewBox="0 0 20 20" aria-hidden="true">
                        <path d="M12.5 4.5 7 10l5.5 5.5" />
                      </svg>
                    </button>
                    <button class="image-arrow right is-visible" data-edit-image-nav="next" type="button" aria-label="Sledeca slika">
                      <svg viewBox="0 0 20 20" aria-hidden="true">
                        <path d="M7.5 4.5 13 10l-5.5 5.5" />
                      </svg>
                    </button>
                    <div class="image-count" id="editImageCount">${safeImageIndex + 1}/${imageCount}</div>
                  `
                  : ""
              }
            </div>

            <div class="edit-hero-body">
              <div class="edit-copy">
                <p class="edit-eyebrow">PROMJENA ARTIKLA</p>
                ${
                  item
                    ? `
                      <div class="edit-fixed-meta">
                        <div class="edit-fixed-box">
                          <span>INDEX</span>
                          <strong>${escapeHtml(item.index || "-")}</strong>
                        </div>
                        <div class="edit-fixed-box">
                          <span>BREND</span>
                          <strong>${escapeHtml(item.brandName || "-")}</strong>
                        </div>
                      </div>
                    `
                    : ""
                }
              </div>

              ${
                state.isLoading
                  ? `<div class="edit-state">Ucitavam podatke...</div>`
                  : item
                    ? `
                      <form class="edit-form edit-form-large" id="editForm">
                        <label class="edit-field">
                          <span class="edit-label-tight">Ime</span>
                          <input id="editName" name="name" type="text" value="${escapeHtml(item.nameB2B || "")}" required />
                        </label>

                        <label class="edit-field">
                          <span>Cijena</span>
                          <input id="editPrice" name="price" type="number" min="0" step="0.01" value="${escapeHtml(priceValue)}" required />
                        </label>

                        <label class="edit-field edit-field-fill">
                          <span class="edit-label-tight">Deskripcija</span>
                          <textarea id="editDescription" name="description" rows="10">${escapeHtml(item.technicalDescription || "")}</textarea>
                        </label>

                        <div class="edit-card-footer">
                          <button class="ghost-button" type="button" id="backButton">Nazad</button>
                          <button class="save-button" type="submit" ${state.isSaving ? "disabled" : ""}>
                            ${state.isSaving ? "Cuvam..." : "Sacuvaj"}
                          </button>
                        </div>
                      </form>
                    `
                    : `<div class="edit-state ${state.tone === "danger" ? "danger" : ""}">${escapeHtml(state.message || "Artikal nije pronađen.")}</div>`
              }
            </div>
          </article>
        </section>
      </main>
    </div>
  `;

  bindEditEvents(root, towKod);
}

function bindEditEvents(root: HTMLDivElement, towKod: string): void {
  const backButton = document.querySelector<HTMLButtonElement>("#backButton");
  const editForm = document.querySelector<HTMLFormElement>("#editForm");
  const imageButtons =
    document.querySelectorAll<HTMLButtonElement>("[data-edit-image-nav]");

  backButton?.addEventListener("click", () => {
    navigateWithFade(root, "/");
  });

  for (const button of imageButtons) {
    button.addEventListener("click", () => {
      if (!state.item || state.item.images.length < 2) {
        return;
      }

      const direction = button.dataset.editImageNav;
      const delta = direction === "next" ? 1 : -1;
      state.activeImageIndex =
        (state.activeImageIndex + delta + state.item.images.length) %
        state.item.images.length;
      updateEditImage();
    });
  }

  editForm?.addEventListener("submit", async (event) => {
    event.preventDefault();

    const nameInput = document.querySelector<HTMLInputElement>("#editName");
    const priceInput = document.querySelector<HTMLInputElement>("#editPrice");
    const descriptionInput =
      document.querySelector<HTMLTextAreaElement>("#editDescription");

    if (!nameInput || !priceInput || !descriptionInput) {
      return;
    }

    state.isSaving = true;
    renderEditPage(root, towKod);

    try {
      const response = await fetch(`/api/catalog/item/${encodeURIComponent(towKod)}`, {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          name: nameInput.value,
          price: Number(priceInput.value),
          description: descriptionInput.value,
        }),
      });

      const data = (await response.json()) as CatalogItem | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "Cuvanje nije uspelo.");
      }

      state.item = data as CatalogItem;
      navigateWithFade(root, "/");
    } catch (error) {
      state.message =
        error instanceof Error ? error.message : "Cuvanje nije uspelo.";
      state.tone = "danger";
      state.isSaving = false;
      renderEditPage(root, towKod);
    }
  });
}

function updateEditImage(): void {
  if (!state.item || state.item.images.length === 0) {
    return;
  }

  const currentImage = state.item.images[state.activeImageIndex];
  const imageElement = document.querySelector<HTMLImageElement>("#editImage");
  const countElement =
    document.querySelector<HTMLDivElement>("#editImageCount");

  if (imageElement && currentImage) {
    imageElement.classList.add("edit-image-fade");
    imageElement.src = currentImage.url;
    imageElement.alt = state.item.nameB2B || state.activeTowKod;
    window.setTimeout(() => {
      imageElement.classList.remove("edit-image-fade");
    }, 140);
  }

  if (countElement) {
    countElement.textContent = `${state.activeImageIndex + 1}/${state.item.images.length}`;
  }
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

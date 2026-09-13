import { navigateWithFade } from "./transition";
import { renderPublishSessionBadge } from "./publish-session";
import { renderSessionBadge } from "./session";
import { renderAppBrand } from "./branding";
import Quill from "quill";
import "quill/dist/quill.snow.css";

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
	itemId: string;
  towKod: string;
  index: string;
  nameB2B: string;
  brandName: string;
  images: CatalogImage[];
	processedImageUrl?: string;
  technicalDescription: string;
	aiTechnicalDescription?: string;
	useAIContent?: boolean;
  technicalDescriptionFormat?: "plain" | "html";
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
  sessionUsername: string;
  tokenConfigured: boolean;
  quill: Quill | null;
} = {
  item: null,
  isLoading: false,
  isSaving: false,
  message: "",
  tone: "muted",
  activeTowKod: "",
  activeImageIndex: 0,
  sessionUsername: "",
  tokenConfigured: false,
  quill: null,
};

export function mountEditPage(root: HTMLDivElement, towKod: string): void {
  if (!state.sessionUsername) {
    state.sessionUsername = "";
  }

  if (!state.isLoading) {
    if (state.activeTowKod !== towKod) {
      state.activeTowKod = towKod;
      state.activeImageIndex = 0;
    }
    // The AI can finish while the user is elsewhere in the app. Always reload
    // on entering this page so the editor never keeps an old supplier version.
    void loadEditItem(root, towKod);
  }

  renderEditPage(root, towKod);
}

export function setEditPageSession(sessionUsername: string, tokenConfigured: boolean): void {
  state.sessionUsername = sessionUsername;
  state.tokenConfigured = tokenConfigured;
}

async function loadEditItem(root: HTMLDivElement, towKod: string): Promise<void> {
  state.isLoading = true;
  state.item = null;
  state.quill = null;
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
  const displayImages = item?.processedImageUrl
    ? [{ position: -1, url: item.processedImageUrl }, ...item.images]
    : item?.images ?? [];
  const imageCount = displayImages.length;
  const safeImageIndex =
    imageCount > 0 ? Math.min(state.activeImageIndex, imageCount - 1) : 0;
  const image = displayImages[safeImageIndex]?.url ?? "";
  const priceValue =
    item && Number.isFinite(item.retailPrice) ? String(item.retailPrice) : "0";
  const usesHtmlDescription =
    item !== null && descriptionFormatForEdit(item) === "html";

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
                imageCount > 0
                  ? `
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
                    `
                        : ""
                    }
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

                        <div class="edit-field edit-field-fill${usesHtmlDescription ? " edit-field-rich" : ""}">
                          <span class="edit-label-tight" id="editDescriptionLabel">${isEditingAIDescription(item) ? "AI deskripcija proizvoda" : "Deskripcija proizvoda"}</span>
                          ${usesHtmlDescription
                            ? `<div id="editDescriptionEditor" class="description-editor"></div>`
                            : `<textarea id="editDescription" name="description" rows="10" aria-labelledby="editDescriptionLabel">${escapeHtml(descriptionForEdit(item))}</textarea>`}
                        </div>

                        <div class="edit-card-footer">
                          <button class="ghost-button edit-action-button edit-back-button" type="button" id="backButton">
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
                    : `<div class="edit-state ${state.tone === "danger" ? "danger" : ""}">${escapeHtml(state.message || "Artikal nije pronađen.")}</div>`
              }
            </div>
          </article>
        </section>
      </main>
    </div>
  `;

  bindEditEvents(root, towKod);
  mountEditDescriptionEditor();
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
      const displayImages = state.item?.processedImageUrl
        ? [{ position: -1, url: state.item.processedImageUrl }, ...state.item.images]
        : state.item?.images ?? [];
      if (!state.item || displayImages.length < 2) {
        return;
      }

      const direction = button.dataset.editImageNav;
      const delta = direction === "next" ? 1 : -1;
      state.activeImageIndex =
        (state.activeImageIndex + delta + displayImages.length) %
        displayImages.length;
      updateEditImage();
    });
  }

  editForm?.addEventListener("submit", async (event) => {
    event.preventDefault();

    const nameInput = document.querySelector<HTMLInputElement>("#editName");
    const priceInput = document.querySelector<HTMLInputElement>("#editPrice");
    const descriptionInput =
      document.querySelector<HTMLTextAreaElement>("#editDescription");

    if (!nameInput || !priceInput || (!descriptionInput && !state.quill)) {
      return;
    }

    const descriptionValue = state.quill
      ? normalizeEditorHtml(state.quill.getSemanticHTML())
      : descriptionInput?.value || "";

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
          description: descriptionValue,
          descriptionFormat: state.quill ? "html" : "plain",
        }),
      });

      const data = (await response.json()) as CatalogItem | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "Cuvanje nije uspelo.");
      }

      state.item = data as CatalogItem;
      state.isSaving = false;
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

function mountEditDescriptionEditor(): void {
  const container = document.querySelector<HTMLDivElement>("#editDescriptionEditor");
	if (!container || !state.item || descriptionFormatForEdit(state.item) !== "html") {
    state.quill = null;
    return;
  }
  const quill = new Quill(container, {
    theme: "snow",
    modules: {
      toolbar: [
        [{ header: [2, 3, false] }],
        ["bold", "italic"],
        [{ list: "ordered" }, { list: "bullet" }],
        ["clean"],
      ],
    },
    placeholder: "Unesi deskripciju...",
  });
  quill.clipboard.dangerouslyPasteHTML(descriptionForEdit(state.item));
  quill.root.setAttribute("aria-labelledby", "editDescriptionLabel");
  state.quill = quill;
}

function isEditingAIDescription(item: CatalogItem): boolean {
	return Boolean(item.useAIContent && item.aiTechnicalDescription?.trim());
}

function descriptionForEdit(item: CatalogItem): string {
	return isEditingAIDescription(item) ? item.aiTechnicalDescription || "" : item.technicalDescription || "";
}

function descriptionFormatForEdit(item: CatalogItem): "plain" | "html" {
	return isEditingAIDescription(item) ? "html" : item.technicalDescriptionFormat === "html" ? "html" : "plain";
}

function normalizeEditorHtml(value: string): string {
  const normalized = value.trim();
  return normalized === "<p><br></p>" || normalized === "<p></p>" ? "" : normalized;
}

function updateEditImage(): void {
	const displayImages = state.item?.processedImageUrl
		? [{ position: -1, url: state.item.processedImageUrl }, ...state.item.images]
		: state.item?.images ?? [];
	if (!state.item || displayImages.length === 0) {
		return;
	}

	const currentImage = displayImages[state.activeImageIndex];
  const imageElement = document.querySelector<HTMLImageElement>("#editImage");
  const countElement =
    document.querySelector<HTMLDivElement>("#editImageCount");

	if (imageElement && currentImage) {
		const nextImage = new Image();
		nextImage.onload = () => {
			if (state.activeImageIndex !== displayImages.indexOf(currentImage)) return;
			imageElement.src = currentImage.url;
			imageElement.alt = state.item?.nameB2B || state.activeTowKod;
		};
		nextImage.src = currentImage.url;
	}

  if (countElement) {
		countElement.textContent = `${state.activeImageIndex + 1}/${displayImages.length}`;
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

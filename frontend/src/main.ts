import "./style.css";

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

type CatalogResponse = CatalogState | ErrorResponse;

const app = document.querySelector<HTMLDivElement>("#app");

if (!app) {
  throw new Error("No #app element found.");
}

const root = app;

const state: {
  data: CatalogState;
  isDragging: boolean;
  isUploading: boolean;
  message: string;
  messageTone: "muted" | "success" | "danger";
  imageIndexes: Record<string, number>;
} = {
  data: {
    catalogs: [],
    items: [],
    tempDir: "",
    totalFiles: 0,
    totalItems: 0,
    lastUpdated: null,
  },
  isDragging: false,
  isUploading: false,
  message: "Prevuci JSON fajl ili klikni na Dodaj jos.",
  messageTone: "muted",
  imageIndexes: {},
};

const plusIcon = `
  <svg viewBox="0 0 20 20" aria-hidden="true">
    <path d="M10 4.5v11M4.5 10h11" />
  </svg>
`;

const editIcon = `
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M20 14.66V20a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h5.34" />
    <path d="M18 2 22 6 12 16 8 16 8 12 18 2" />
  </svg>
`;

const categorizeIcon = `
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M17 10h2c2 0 3-1 3-3V5c0-2-1-3-3-3H17c-2 0-3 1-3 3V7c0 2 1 3 3 3z" />
    <path d="M5 22H7c2 0 3-1 3-3V17c0-2-1-3-3-3H5c-2 0-3 1-3 3v2c0 2 1 3 3 3z" />
    <path d="M6 10c2.209 0 4-1.791 4-4S8.209 2 6 2 2 3.791 2 6s1.791 4 4 4z" opacity="0.34" />
    <path d="M18 22c2.209 0 4-1.791 4-4s-1.791-4-4-4-4 1.791-4 4 1.791 4 4 4z" opacity="0.34" />
  </svg>
`;

function render(): void {
  const hasItems = state.data.items.length > 0;

  const cards = state.data.items
    .map((item) => {
      const imageKey = item.towKod || item.index || item.nameB2B;
      const currentImageIndex = Math.min(
        state.imageIndexes[imageKey] ?? 0,
        Math.max(item.images.length - 1, 0),
      );
      const image = item.images[currentImageIndex]?.url ?? "";
      const price =
        item.retailPrice > 0
          ? new Intl.NumberFormat("sr-Latn-RS", {
              style: "currency",
              currency: item.currencyCode || "EUR",
              maximumFractionDigits: 2,
            }).format(item.retailPrice)
          : "Na upit";

      return `
        <article class="product-card">
          <div class="product-media">
            ${
              image
                ? `<img src="${image}" alt="${escapeHtml(item.nameB2B)}" loading="lazy" />`
                : `<div class="product-placeholder">Nema slike</div>`
            }
            ${
              item.images.length > 1
                ? `
                  <button class="image-arrow left" data-image-nav="prev" data-image-key="${escapeHtml(imageKey)}" type="button" aria-label="Prethodna slika">
                    <svg viewBox="0 0 20 20" aria-hidden="true">
                      <path d="M12.5 4.5 7 10l5.5 5.5" />
                    </svg>
                  </button>
                  <button class="image-arrow right" data-image-nav="next" data-image-key="${escapeHtml(imageKey)}" type="button" aria-label="Sledeca slika">
                    <svg viewBox="0 0 20 20" aria-hidden="true">
                      <path d="M7.5 4.5 13 10l-5.5 5.5" />
                    </svg>
                  </button>
                `
                : ""
            }
            ${
              item.images.length > 0
                ? `<div class="image-count">${currentImageIndex + 1}/${item.images.length}</div>`
                : ""
            }
          </div>
          <div class="product-body">
            <h3>${escapeHtml(item.nameB2B || "Artikal bez naziva")}</h3>
            <div class="product-line">
              <span>${escapeHtml(item.brandName || "Nepoznat brend")}</span>
              <span>${escapeHtml(item.index || item.towKod || "Bez sifre")}</span>
            </div>
            <p>${escapeHtml(item.technicalDescription || "Nema opisa.")}</p>
            <div class="product-line bottom">
              <strong>${price}</strong>
              <div class="card-actions">
                <button class="icon-button" type="button" aria-label="Kategorizacija">
                  ${categorizeIcon}
                </button>
                <button class="icon-button" type="button" aria-label="Promjeni">
                  ${editIcon}
                </button>
              </div>
            </div>
          </div>
        </article>
      `;
    })
    .join("");

  root.innerHTML = `
    <div class="shell">
      <header class="topbar">
        <div class="logo-wrap">
          <div class="logo-mark">IC</div>
          <strong>ICAI</strong>
        </div>
        <button class="primary-button" id="uploadButton" ${
          state.isUploading ? "disabled" : ""
        }>
          <span class="button-icon">${plusIcon}</span>
          Dodaj jos
        </button>
      </header>

      <main class="main-layout">
        <section class="drop-layer ${state.isDragging ? "dragging" : ""}" id="dropZone">
        <input id="fileInput" type="file" accept=".json,application/json" multiple hidden />

        ${
          hasItems
            ? `
              <section class="products-grid">
                ${cards}
              </section>
            `
            : `
              <section class="drop-simple">
                <button class="drop-simple-button" id="browseButton" ${
                  state.isUploading ? "disabled" : ""
                }>
                  <span class="button-icon">${plusIcon}</span>
                </button>
                <h1>UBACI ARTIKLE</h1>
                <p class="${state.messageTone}">${escapeHtml(state.message)}</p>
              </section>
            `
        }

        ${
          hasItems
            ? `
              <section class="drop-inline">
                <button class="drop-inline-button" id="browseButton" ${
                  state.isUploading ? "disabled" : ""
                }><span class="button-icon">${plusIcon}</span>Dodaj jos</button>
              </section>
            `
            : ""
        }
        </section>
      </main>

      <footer class="footer-bar">
        <button class="danger-button" id="clearButton">Obrisi</button>
        <button class="publish-button" type="button">Objavi</button>
      </footer>
    </div>
  `;

  bindEvents();
}

function bindEvents(): void {
  const fileInput = document.querySelector<HTMLInputElement>("#fileInput");
  const uploadButton =
    document.querySelector<HTMLButtonElement>("#uploadButton");
  const browseButton =
    document.querySelector<HTMLButtonElement>("#browseButton");
  const dropZone = document.querySelector<HTMLElement>("#dropZone");
  const clearButton = document.querySelector<HTMLButtonElement>("#clearButton");
  const imageButtons =
    document.querySelectorAll<HTMLButtonElement>("[data-image-nav]");

  if (!fileInput || !uploadButton || !clearButton) {
    return;
  }

  const openPicker = () => fileInput.click();
  uploadButton.addEventListener("click", openPicker);
  browseButton?.addEventListener("click", openPicker);

  fileInput.addEventListener("change", async () => {
    const files = fileInput.files ? Array.from(fileInput.files) : [];
    await uploadFiles(files);
    fileInput.value = "";
  });

  if (dropZone) {
    dropZone.addEventListener("dragenter", (event) => {
      event.preventDefault();
      state.isDragging = true;
      render();
    });

    dropZone.addEventListener("dragover", (event) => {
      event.preventDefault();
    });

    dropZone.addEventListener("dragleave", (event) => {
      event.preventDefault();
      const target = event.relatedTarget;
      if (!(target instanceof Node) || !dropZone.contains(target)) {
        state.isDragging = false;
        render();
      }
    });

    dropZone.addEventListener("drop", async (event) => {
      event.preventDefault();
      state.isDragging = false;
      render();
      const files = Array.from(event.dataTransfer?.files ?? []);
      await uploadFiles(files);
    });
  }

  clearButton.addEventListener("click", async () => {
    clearButton.disabled = true;
    try {
      const response = await fetch("/api/catalog", { method: "DELETE" });
      const data = (await response.json()) as CatalogResponse;

      if (!response.ok) {
        throw new Error("error" in data ? data.error : "Brisanje nije uspelo.");
      }

      state.data = data as CatalogState;
      state.imageIndexes = {};
      state.message = "Sve je obrisano.";
      state.messageTone = "success";
    } catch (error) {
      state.message =
        error instanceof Error ? error.message : "Brisanje nije uspelo.";
      state.messageTone = "danger";
    } finally {
      clearButton.disabled = false;
      render();
    }
  });

  for (const button of imageButtons) {
    button.addEventListener("click", () => {
      const direction = button.dataset.imageNav;
      const imageKey = button.dataset.imageKey;

      if (!direction || !imageKey) {
        return;
      }

      const item = state.data.items.find(
        (entry) => (entry.towKod || entry.index || entry.nameB2B) === imageKey,
      );

      if (!item || item.images.length < 2) {
        return;
      }

      const currentIndex = state.imageIndexes[imageKey] ?? 0;
      const delta = direction === "next" ? 1 : -1;
      const nextIndex =
        (currentIndex + delta + item.images.length) % item.images.length;
      state.imageIndexes[imageKey] = nextIndex;
      render();
    });
  }
}

async function loadState(): Promise<void> {
  try {
    const response = await fetch("/api/catalog");
    const data = (await response.json()) as CatalogResponse;

    if (!response.ok) {
      throw new Error("error" in data ? data.error : "Ucitavanje nije uspelo.");
    }

    state.data = data as CatalogState;
    state.imageIndexes = {};
    if (state.data.totalItems > 0) {
      state.message = "Artikli su ucitani.";
      state.messageTone = "success";
    }
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Backend nije dostupan.";
    state.messageTone = "danger";
  } finally {
    render();
  }
}

async function uploadFiles(files: File[]): Promise<void> {
  const jsonFiles = files.filter((file) =>
    file.name.toLowerCase().endsWith(".json"),
  );
  if (jsonFiles.length === 0) {
    state.message = "Izaberi barem jedan .json fajl.";
    state.messageTone = "danger";
    render();
    return;
  }

  state.isUploading = true;
  state.message = "Ucitavam artikle...";
  state.messageTone = "muted";
  render();

  try {
    const formData = new FormData();
    for (const file of jsonFiles) {
      formData.append("files", file);
    }

    const response = await fetch("/api/catalog/upload", {
      method: "POST",
      body: formData,
    });

    const data = (await response.json()) as CatalogResponse;
    if (!response.ok) {
      throw new Error("error" in data ? data.error : "Upload nije uspeo.");
    }

    state.data = data as CatalogState;
    for (const item of state.data.items) {
      const imageKey = item.towKod || item.index || item.nameB2B;
      if (!(imageKey in state.imageIndexes)) {
        state.imageIndexes[imageKey] = 0;
      }
    }
    state.message = "Artikli su dodati.";
    state.messageTone = "success";
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Upload nije uspeo.";
    state.messageTone = "danger";
  } finally {
    state.isUploading = false;
    render();
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

void loadState();

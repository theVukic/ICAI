import { navigateWithFade } from "./transition";
import { loadSessionInfo, renderSessionBadge } from "./session";

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

const searchIcon = `
  <svg viewBox="144 144 512 512" aria-hidden="true">
    <path d="m540.8 490.93c-5.0508-5.0391-11.512-8.4297-18.531-9.7227-7.0195-1.293-14.266-0.42578-20.781 2.4883l-27.27-27.266c28.961-33.586 43.77-77.09 41.312-121.37-2.4531-44.277-21.984-85.879-54.48-116.05-32.496-30.176-75.43-46.574-119.77-45.75-44.336 0.82422-86.629 18.812-117.98 50.176-31.352 31.367-49.316 73.664-50.125 118-0.80469 44.34 15.613 87.266 45.805 119.75 30.191 32.484 71.801 51.996 116.08 54.434 44.281 2.4336 87.781-12.395 121.35-41.367l27.262 27.258h0.003906c-2.9141 6.5156-3.7852 13.762-2.4922 20.777 1.293 7.0195 4.6875 13.48 9.7305 18.527l75.586 75.57v0.003907c8.9102 8.9102 21.895 12.391 34.07 9.1328 12.172-3.2617 21.68-12.77 24.941-24.941 3.2617-12.172-0.21484-25.16-9.125-34.07zm-196.27-20.402c-39.156-0.003906-76.082-18.219-99.918-49.289-23.832-31.066-31.859-71.453-21.723-109.27s37.289-68.773 73.465-83.758c36.176-14.98 77.266-12.285 111.18 7.293 33.906 19.582 56.781 53.816 61.891 92.637 5.1133 38.824-8.125 77.812-35.812 105.5-23.574 23.699-55.648 36.984-89.078 36.891z"/>
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
  categorization?: ItemCategorySelection;
};

type CatalogStateResponse = {
  items: CatalogItem[];
};

type OLXCategoryNode = {
  id: number;
  name: string;
  name_singular?: string;
  slug?: string;
  show_brand?: boolean;
  brand_required?: boolean;
  model_required?: boolean;
  has_models?: boolean;
};

type OLXCategoryAttribute = {
  id: number;
  type: string;
  name: string;
  input_type: string;
  display_name: string;
  options: string[];
  required: boolean;
  order: number;
};

type OLXBrand = {
  id: number;
  name: string;
  slug?: string;
};

type OLXModel = {
  id: number;
  name: string;
  slug?: string;
};

type ItemCategorySelection = {
  category?: OLXCategoryNode;
  brand?: OLXBrand;
  model?: OLXModel;
  attributes?: Record<string, string>;
};

type OLXCategorySuggestion = {
  id: number;
  name: string;
  path?: string;
  count?: number;
  parent_categories?: string[];
};

type ErrorResponse = {
  error: string;
};

const state: {
  items: CatalogItem[];
  tokenConfigured: boolean;
  searchQuery: string;
  searchResults: OLXCategorySuggestion[];
  selectedCategory: OLXCategoryNode | null;
  attributes: OLXCategoryAttribute[];
  brands: OLXBrand[];
  models: OLXModel[];
  selectedBrandId: string;
  selectedModelId: string;
  attributeValues: Record<string, string>;
  isLoading: boolean;
  isSaving: boolean;
  isSearching: boolean;
  message: string;
  tone: "muted" | "danger" | "success";
  sessionUsername: string;
  validationMessage: string;
  invalidFields: string[];
} = {
  items: [],
  tokenConfigured: false,
  searchQuery: "",
  searchResults: [],
  selectedCategory: null,
  attributes: [],
  brands: [],
  models: [],
  selectedBrandId: "",
  selectedModelId: "",
  attributeValues: {},
  isLoading: false,
  isSaving: false,
  isSearching: false,
  message: "",
  tone: "muted",
  sessionUsername: "",
  validationMessage: "",
  invalidFields: [],
};

export function mountBulkCategoryPage(root: HTMLDivElement): void {
  if (!state.isLoading && state.items.length === 0) {
    void loadBulkCategoryItems(root);
  }

  renderBulkCategoryPage(root);
}

async function loadBulkCategoryItems(root: HTMLDivElement): Promise<void> {
  state.isLoading = true;
  state.message = "";
  renderBulkCategoryPage(root);

  try {
    const session = await loadSessionInfo();
    state.sessionUsername = session.username || "";
    state.tokenConfigured = session.tokenConfigured;

    const selectedTowKods = loadStoredSelectedTowKods();
    const response = await fetch("/api/catalog");
    const data = (await response.json()) as CatalogStateResponse | ErrorResponse;
    if (!response.ok) {
      throw new Error("error" in data ? data.error : "Ucitavanje nije uspelo.");
    }

    const catalogState = data as CatalogStateResponse;
    const selectedSet = new Set(selectedTowKods);
    state.items = catalogState.items.filter((item) => selectedSet.has(item.towKod));

    if (state.items.length === 0) {
      state.message = "Nema odabranih artikala za kategorizaciju.";
      state.tone = "danger";
      return;
    }

    state.searchQuery = state.items[0]?.nameB2B || "";
    if (state.tokenConfigured && state.searchQuery.trim().length >= 2) {
      await runSuggest(root, state.searchQuery, false);
    }
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Ucitavanje nije uspelo.";
    state.tone = "danger";
  } finally {
    state.isLoading = false;
    renderBulkCategoryPage(root);
  }
}

function renderBulkCategoryPage(root: HTMLDivElement): void {
  const hasItems = state.items.length > 0;
  const selectedBrand = state.brands.find(
    (brand) => String(brand.id) === state.selectedBrandId,
  );
  const selectedModel = state.models.find(
    (model) => String(model.id) === state.selectedModelId,
  );

  root.innerHTML = `
    <div class="shell edit-shell">
      <header class="topbar edit-topbar">
        <div class="logo-wrap">
          <div class="logo-mark">IC</div>
          <strong>ICAI</strong>
          ${renderSessionBadge(state.tokenConfigured ? { tokenConfigured: true, username: state.sessionUsername, fullName: "" } : null)}
        </div>
      </header>

      <main class="edit-page bulk-edit-page">
        <section class="bulk-edit-layout">
          <aside class="bulk-preview-panel">
            <div class="edit-copy">
              <p class="edit-eyebrow">ODABRANI ARTIKLI</p>
              <div class="bulk-preview-summary">
                <strong>${hasItems ? `${state.items.length}` : "0"}</strong>
                <span>${hasItems ? "artikala za kategorizaciju" : "nema odabranih artikala"}</span>
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
            <article class="edit-hero-card bulk-form-card category-bulk-card">
              <div class="edit-hero-body bulk-form-body category-bulk-body">
                ${
                  !state.tokenConfigured && hasItems
                    ? `<div class="category-note danger">OLX token nije podešen. Zalijepi ga u backend/token.json pa ponovo otvori kategorizaciju.</div>`
                    : ""
                }

                ${
                  state.isLoading
                    ? `<div class="edit-state">Ucitavam kategorizaciju...</div>`
                    : hasItems
                      ? `
                        <div class="bulk-form-intro">
                          <p class="edit-eyebrow">GRUPNA KATEGORIZACIJA</p>
                          <p class="edit-subtitle">Izaberi jednu kategoriju i ona ce biti sacuvana za sve odabrane artikle.</p>
                        </div>

                        <section class="category-search">
                          <div class="category-search-row">
                            <label class="edit-field category-search-field">
                              <span>Kategorija</span>
                              <input id="categorySearchInput" type="text" value="${escapeHtml(state.searchQuery)}" placeholder="Upisi naziv kategorije" ${!state.tokenConfigured ? "disabled" : ""} />
                            </label>
                            <div class="category-search-actions">
                              <button class="save-button category-search-button" type="button" id="searchCategoryButton" ${!state.tokenConfigured ? "disabled" : ""}>
                                <span class="edit-action-icon">${searchIcon}</span>
                                <span>Pronadji</span>
                              </button>
                            </div>
                          </div>
                          ${
                            state.searchResults.length > 0
                              ? `
                                <div class="category-results">
                                  ${state.searchResults
                                    .map(
                                      (result) => `
                                        <button class="category-result" type="button" data-category-id="${result.id}" data-category-name="${escapeHtml(result.name)}">
                                          <div class="category-result-title">${highlightMatch(result.name, state.searchQuery)}</div>
                                          <span>${highlightMatch(result.path || (result.parent_categories || []).join(" > ") || "", state.searchQuery)}</span>
                                        </button>
                                      `,
                                    )
                                    .join("")}
                                </div>
                              `
                              : state.isSearching
                                ? `<div class="category-note category-loading">Tražim kategorije...</div>`
                                : ""
                          }
                        </section>

                        ${
                          state.selectedCategory
                            ? `
                              <section class="category-selected">
                                <div class="category-chip">
                                  <span>Izabrana kategorija</span>
                                  <strong>${escapeHtml(state.selectedCategory.name)}</strong>
                                </div>

                                ${
                                  state.selectedCategory.show_brand
                                    ? `
                                      <label class="edit-field">
                                        <span>Brend iz kategorije${state.selectedCategory.brand_required ? " *" : ""}</span>
                                        <select id="categoryBrandSelect" class="${state.invalidFields.includes("brand") ? "field-invalid" : ""}">
                                          <option value="">Izaberi brend</option>
                                          ${state.brands
                                            .map(
                                              (brand) => `
                                                <option value="${brand.id}" ${String(brand.id) === state.selectedBrandId ? "selected" : ""}>${escapeHtml(brand.name)}</option>
                                              `,
                                            )
                                            .join("")}
                                        </select>
                                      </label>
                                    `
                                    : ""
                                }

                                ${
                                  state.selectedCategory.has_models
                                    ? `
                                      <label class="edit-field">
                                        <span>Model${state.selectedCategory.model_required ? " *" : ""}</span>
                                        <select id="categoryModelSelect" class="${state.invalidFields.includes("model") ? "field-invalid" : ""}" ${state.selectedBrandId ? "" : "disabled"}>
                                          <option value="">Izaberi model</option>
                                          ${state.models
                                            .map(
                                              (model) => `
                                                <option value="${model.id}" ${String(model.id) === state.selectedModelId ? "selected" : ""}>${escapeHtml(model.name)}</option>
                                              `,
                                            )
                                            .join("")}
                                        </select>
                                      </label>
                                    `
                                    : ""
                                }

                                ${
                                  state.attributes.length > 0
                                    ? `
                                      <div class="category-attributes">
                                        ${state.attributes
                                          .slice()
                                          .sort((a, b) => a.order - b.order)
                                          .map((attribute) =>
                                            renderAttributeField(attribute, state.attributeValues[attribute.name] || ""),
                                          )
                                          .join("")}
                                      </div>
                                    `
                                    : `<div class="category-note">Za ovu kategoriju nema dodatnih atributa.</div>`
                                }
                              </section>
                            `
                            : ""
                        }

                        ${
                          state.message
                            ? `<div class="category-note ${state.tone === "danger" ? "danger" : state.tone === "success" ? "success" : ""}">${escapeHtml(state.message)}</div>`
                            : ""
                        }

                        <div class="edit-card-footer">
                          <button class="ghost-button edit-action-button edit-back-button" type="button" id="bulkCategoryBackButton">
                            <span class="edit-action-icon">${backIcon}</span>
                            <span>Nazad</span>
                          </button>
                          <button class="save-button" type="button" id="bulkCategorySaveButton" ${state.selectedCategory && state.tokenConfigured ? "" : "disabled"}>
                            <span class="edit-action-icon">${saveIcon}</span>
                            <span>${state.isSaving ? "Cuvam..." : "Sacuvaj kategoriju"}</span>
                          </button>
                        </div>
                      `
                      : `
                        <div class="edit-state ${state.tone === "danger" ? "danger" : ""}">
                          ${escapeHtml(state.message || "Nema odabranih artikala za kategorizaciju.")}
                        </div>
                        <div class="edit-card-footer">
                          <button class="ghost-button edit-action-button edit-back-button" type="button" id="bulkCategoryBackButton">
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

  bindBulkCategoryEvents(root, selectedBrand, selectedModel);
}

function renderAttributeField(attribute: OLXCategoryAttribute, value: string): string {
  const label = `${escapeHtml(attribute.display_name || attribute.name)}${attribute.required ? " *" : ""}`;
  const fieldId = `bulk-category-attr-${attribute.id}`;

  if (attribute.input_type === "select") {
    return `
      <label class="edit-field">
        <span>${label}</span>
        <select data-attribute-name="${escapeHtml(attribute.name)}" id="${fieldId}" class="${state.invalidFields.includes(attribute.name) ? "field-invalid" : ""}">
          <option value="">Izaberi</option>
          ${attribute.options
            .map(
              (option) => `
                <option value="${escapeHtml(option)}" ${option === value ? "selected" : ""}>${escapeHtml(option)}</option>
              `,
            )
            .join("")}
        </select>
      </label>
    `;
  }

  if (attribute.input_type === "checkbox" || attribute.type === "boolean") {
    return `
      <label class="category-checkbox ${state.invalidFields.includes(attribute.name) ? "field-invalid" : ""}">
        <input type="checkbox" data-attribute-name="${escapeHtml(attribute.name)}" ${value === "true" ? "checked" : ""} />
        <span>${label}</span>
      </label>
    `;
  }

  const inputType =
    attribute.type === "integer" || attribute.type === "number" || attribute.type === "float"
      ? "number"
      : "text";

  return `
    <label class="edit-field">
      <span>${label}</span>
      <input id="${fieldId}" data-attribute-name="${escapeHtml(attribute.name)}" class="${state.invalidFields.includes(attribute.name) ? "field-invalid" : ""}" type="${inputType}" value="${escapeHtml(value)}" />
    </label>
  `;
}

function bindBulkCategoryEvents(
  root: HTMLDivElement,
  selectedBrand: OLXBrand | undefined,
  selectedModel: OLXModel | undefined,
): void {
  const backButton = document.querySelector<HTMLButtonElement>("#bulkCategoryBackButton");
  const saveButton = document.querySelector<HTMLButtonElement>("#bulkCategorySaveButton");
  const searchInput = document.querySelector<HTMLInputElement>("#categorySearchInput");
  const searchButton = document.querySelector<HTMLButtonElement>("#searchCategoryButton");
  const resultButtons = document.querySelectorAll<HTMLButtonElement>("[data-category-id]");
  const brandSelect = document.querySelector<HTMLSelectElement>("#categoryBrandSelect");
  const modelSelect = document.querySelector<HTMLSelectElement>("#categoryModelSelect");
  const attributeInputs = document.querySelectorAll<HTMLElement>("[data-attribute-name]");

  backButton?.addEventListener("click", () => {
    navigateWithFade(root, "/");
  });

  searchInput?.addEventListener("input", () => {
    state.searchQuery = searchInput.value;
    state.validationMessage = "";
    state.invalidFields = [];
  });

  searchInput?.addEventListener("keydown", (event) => {
    if (event.key !== "Enter") {
      return;
    }

    event.preventDefault();
    void runFind(root, searchInput.value || "", true);
  });

  searchButton?.addEventListener("click", () => {
    void runFind(root, searchInput?.value || "", true);
  });

  for (const button of resultButtons) {
    button.addEventListener("click", () => {
      const categoryID = Number(button.dataset.categoryId);
      const categoryName = button.dataset.categoryName || "";
      void selectCategory(root, { id: categoryID, name: categoryName }, undefined, undefined, true);
    });
  }

  brandSelect?.addEventListener("change", async () => {
    state.selectedBrandId = brandSelect.value;
    state.selectedModelId = "";
    state.models = [];
    state.invalidFields = state.invalidFields.filter((field) => field !== "brand" && field !== "model");

    if (state.selectedCategory?.has_models && state.selectedBrandId) {
      await loadModels(state.selectedCategory.id, Number(state.selectedBrandId));
    }

    renderBulkCategoryPage(root);
  });

  modelSelect?.addEventListener("change", () => {
    state.selectedModelId = modelSelect.value;
    state.invalidFields = state.invalidFields.filter((field) => field !== "model");
  });

  for (const input of attributeInputs) {
    const attributeName = input.dataset.attributeName;
    if (!attributeName) {
      continue;
    }

    if (input instanceof HTMLInputElement && input.type === "checkbox") {
      input.addEventListener("change", () => {
        state.attributeValues[attributeName] = input.checked ? "true" : "false";
        state.invalidFields = state.invalidFields.filter((field) => field !== attributeName);
      });
      continue;
    }

    input.addEventListener("change", () => {
      const formValue =
        input instanceof HTMLInputElement || input instanceof HTMLSelectElement
          ? input.value
          : "";
      state.attributeValues[attributeName] = formValue;
      state.invalidFields = state.invalidFields.filter((field) => field !== attributeName);
    });
  }

  saveButton?.addEventListener("click", async () => {
    if (!state.selectedCategory) {
      return;
    }

    const validationResult = validateCategorySelection();
    if (validationResult.message) {
      state.validationMessage = validationResult.message;
      state.invalidFields = validationResult.invalidFields;
      renderBulkCategoryPage(root);
      return;
    }

    state.validationMessage = "";
    state.invalidFields = [];
    state.isSaving = true;
    renderBulkCategoryPage(root);

    try {
      const response = await fetch("/api/category/bulk", {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          towKods: state.items.map((item) => item.towKod),
          category: state.selectedCategory,
          brand: selectedBrand || null,
          model: selectedModel || null,
          attributes: state.attributeValues,
        }),
      });

      const data = (await response.json()) as CatalogItem[] | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "Cuvanje nije uspelo.");
      }

      navigateWithFade(root, "/");
    } catch (error) {
      state.message =
        error instanceof Error ? error.message : "Cuvanje nije uspelo.";
      state.tone = "danger";
      state.isSaving = false;
      renderBulkCategoryPage(root);
    }
  });
}

async function runFind(root: HTMLDivElement, query: string, rerender: boolean): Promise<void> {
  if (!state.tokenConfigured || query.trim().length < 2) {
    state.searchResults = [];
    if (rerender) {
      renderBulkCategoryPage(root);
    }
    return;
  }

  state.isSearching = true;
  if (rerender && state.searchResults.length === 0) {
    renderBulkCategoryPage(root);
  }

  try {
    state.searchResults = await searchCategories(query);
    state.message = state.searchResults.length === 0 ? "Nema rezultata za ovu pretragu." : "";
    state.tone = state.searchResults.length === 0 ? "muted" : state.tone;
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Pretraga nije uspela.";
    state.tone = "danger";
  } finally {
    state.isSearching = false;
    renderBulkCategoryPage(root);
  }
}

async function runSuggest(root: HTMLDivElement, keyword: string, rerender: boolean): Promise<void> {
  if (!state.tokenConfigured || keyword.trim().length < 2) {
    return;
  }

  state.isSearching = true;
  if (rerender && state.searchResults.length === 0) {
    renderBulkCategoryPage(root);
  }

  try {
    state.searchResults = await searchCategories(keyword);
    state.message = state.searchResults.length === 0 ? "Nema prijedloga za ovu pretragu." : "";
    state.tone = state.searchResults.length === 0 ? "muted" : state.tone;
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Sugestija nije uspela.";
    state.tone = "danger";
  } finally {
    state.isSearching = false;
    renderBulkCategoryPage(root);
  }
}

async function searchCategories(query: string): Promise<OLXCategorySuggestion[]> {
  const findResponse = await fetch(`/api/olx/categories/find?name=${encodeURIComponent(query)}`);
  const findData = (await findResponse.json()) as OLXCategorySuggestion[] | ErrorResponse;

  if (!findResponse.ok) {
    throw new Error("error" in findData ? findData.error : "Pretraga nije uspela.");
  }

  const findResults = findData as OLXCategorySuggestion[];
  let suggestResults: OLXCategorySuggestion[] = [];

  const shouldSuggest =
    findResults.length === 0 ||
    !findResults.some((result) => scoreCategoryResult(result, query) >= 500);

  if (shouldSuggest) {
    const suggestResponse = await fetch(`/api/olx/categories/suggest?keyword=${encodeURIComponent(query)}`);
    const suggestData = (await suggestResponse.json()) as OLXCategorySuggestion[] | ErrorResponse;

    if (!suggestResponse.ok) {
      throw new Error("error" in suggestData ? suggestData.error : "Sugestija nije uspela.");
    }

    suggestResults = suggestData as OLXCategorySuggestion[];
  }

  const merged = new Map<number, OLXCategorySuggestion>();
  for (const result of findResults) {
    merged.set(result.id, result);
  }
  for (const result of suggestResults) {
    if (!merged.has(result.id)) {
      merged.set(result.id, result);
    }
  }

  return Array.from(merged.values()).sort((left, right) => {
    const scoreDiff = scoreCategoryResult(right, query) - scoreCategoryResult(left, query);
    if (scoreDiff !== 0) {
      return scoreDiff;
    }
    return left.name.localeCompare(right.name);
  });
}

function scoreCategoryResult(result: OLXCategorySuggestion, query: string): number {
  const normalizedQuery = normalizeSearchValue(query);
  const normalizedName = normalizeSearchValue(result.name);
  const normalizedPath = normalizeSearchValue(result.path || "");
  const queryWords = normalizedQuery.split(" ").filter(Boolean);
  const leadingPhrase = queryWords.slice(0, Math.min(2, queryWords.length)).join(" ");
  const significantPhrase = queryWords.slice(0, Math.min(3, queryWords.length)).join(" ");

  let score = 0;
  if (normalizedName === normalizedQuery) {
    score += 1000;
  }
  if (normalizedName.startsWith(normalizedQuery)) {
    score += 1400;
  }
  if (normalizedName.includes(normalizedQuery)) {
    score += 900;
  }
  if (leadingPhrase && normalizedName.includes(leadingPhrase)) {
    score += 520;
  }
  if (significantPhrase && normalizedName.startsWith(significantPhrase)) {
    score += 760;
  }
  if (significantPhrase && normalizedName.includes(significantPhrase)) {
    score += 420;
  }
  if (normalizedPath.includes(normalizedQuery)) {
    score += 240;
  }

  for (const word of queryWords) {
    if (normalizedName.startsWith(word)) {
      score += 180;
    } else if (normalizedName.includes(word)) {
      score += 110;
    }

    if (normalizedPath.includes(word)) {
      score += 40;
    }
  }

  const nameLengthPenalty = Math.max(0, normalizedName.length - normalizedQuery.length);
  score -= Math.min(nameLengthPenalty, 80);

  return score;
}

function normalizeSearchValue(value: string): string {
  return value
    .toLowerCase()
    .normalize("NFD")
    .replace(/\p{Diacritic}/gu, "")
    .replace(/\s+/g, " ")
    .trim();
}

async function selectCategory(
  root: HTMLDivElement,
  category: OLXCategoryNode,
  selectedBrandID?: number,
  selectedModelID?: number,
  rerender = true,
): Promise<void> {
  state.selectedCategory = category;
  state.searchResults = [];
  state.selectedBrandId = selectedBrandID ? String(selectedBrandID) : "";
  state.selectedModelId = selectedModelID ? String(selectedModelID) : "";
  state.attributeValues = state.attributeValues || {};

  try {
    const [categoryResponse, attributesResponse] = await Promise.all([
      fetch(`/api/olx/categories/${category.id}`),
      fetch(`/api/olx/categories/${category.id}/attributes`),
    ]);

    const categoryData = (await categoryResponse.json()) as OLXCategoryNode | ErrorResponse;
    const attributesData = (await attributesResponse.json()) as OLXCategoryAttribute[] | ErrorResponse;

    if (!categoryResponse.ok) {
      throw new Error("error" in categoryData ? categoryData.error : "Ucitavanje kategorije nije uspelo.");
    }

    if (!attributesResponse.ok) {
      throw new Error("error" in attributesData ? attributesData.error : "Ucitavanje atributa nije uspelo.");
    }

    state.selectedCategory = categoryData as OLXCategoryNode;
    state.attributes = attributesData as OLXCategoryAttribute[];
    state.brands = [];
    state.models = [];

    if (state.selectedCategory.show_brand) {
      const brandsResponse = await fetch(`/api/olx/categories/${category.id}/brands`);
      const brandsData = (await brandsResponse.json()) as OLXBrand[] | ErrorResponse;
      if (!brandsResponse.ok) {
        throw new Error("error" in brandsData ? brandsData.error : "Ucitavanje brendova nije uspelo.");
      }

      state.brands = brandsData as OLXBrand[];
    }

    if (state.selectedCategory.has_models && selectedBrandID) {
      await loadModels(category.id, selectedBrandID);
    }

    state.message = "";
    state.tone = "muted";
    state.validationMessage = "";
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Ucitavanje kategorije nije uspelo.";
    state.tone = "danger";
  }

  if (rerender) {
    renderBulkCategoryPage(root);
  }
}

function validateCategorySelection(): { message: string; invalidFields: string[] } {
  if (!state.selectedCategory) {
    return { message: "Izaberi kategoriju da bi sačuvao.", invalidFields: [] };
  }

  if (state.selectedCategory.brand_required && !state.selectedBrandId) {
    return { message: "Izaberi brend da bi sačuvao.", invalidFields: ["brand"] };
  }

  if (state.selectedCategory.model_required && !state.selectedModelId) {
    return { message: "Izaberi model da bi sačuvao.", invalidFields: ["model"] };
  }

  for (const attribute of state.attributes) {
    if (!attribute.required) {
      continue;
    }

    const value = (state.attributeValues[attribute.name] || "").trim();
    if (value === "") {
      return {
        message: `Izaberi ${attribute.display_name || attribute.name} da bi sačuvao.`,
        invalidFields: [attribute.name],
      };
    }
  }

  return { message: "", invalidFields: [] };
}

async function loadModels(categoryID: number, brandID: number): Promise<void> {
  const response = await fetch(`/api/olx/categories/${categoryID}/brands/${brandID}/models`);
  const data = (await response.json()) as OLXModel[] | ErrorResponse;
  if (!response.ok) {
    throw new Error("error" in data ? data.error : "Ucitavanje modela nije uspelo.");
  }

  state.models = data as OLXModel[];
}

function loadStoredSelectedTowKods(): string[] {
  try {
    const rawValue = window.localStorage.getItem(SELECTED_TOWKODS_STORAGE_KEY);
    if (!rawValue) {
      return [];
    }

    const parsed = JSON.parse(rawValue) as unknown;
    if (!Array.isArray(parsed)) {
      return [];
    }

    return parsed
      .filter((value): value is string => typeof value === "string")
      .map((value) => value.trim())
      .filter((value) => value.length > 0);
  } catch {
    return [];
  }
}

function formatPrice(value: number, currencyCode: string): string {
  const safeValue = Number.isFinite(value) ? value : 0;
  const rounded = safeValue.toFixed(2);
  const normalized = rounded.endsWith("00")
    ? String(Math.trunc(safeValue))
    : rounded.endsWith("0")
      ? rounded.slice(0, -1)
      : rounded;
  const suffix = currencyCode && currencyCode.trim().length > 0 ? currencyCode : "KM";
  return `${normalized} ${suffix}`;
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function highlightMatch(value: string, query: string): string {
  if (!value || !query.trim()) {
    return escapeHtml(value);
  }

  const matches = buildHighlightRanges(value, query);
  if (matches.length === 0) {
    return escapeHtml(value);
  }

  let output = "";
  let cursor = 0;

  for (const match of matches) {
    if (match.start > cursor) {
      output += escapeHtml(value.slice(cursor, match.start));
    }

    output += `<strong class="search-highlight">${escapeHtml(value.slice(match.start, match.end))}</strong>`;
    cursor = match.end;
  }

  if (cursor < value.length) {
    output += escapeHtml(value.slice(cursor));
  }

  return output;
}

function buildHighlightRanges(
  value: string,
  query: string,
): Array<{ start: number; end: number }> {
  const ranges: Array<{ start: number; end: number }> = [];
  const segments = query
    .trim()
    .split(/\s+/)
    .filter((part) => part.length > 1);

  const combined = segments.slice(0, Math.min(2, segments.length)).join(" ");
  const candidates = [combined, ...segments]
    .filter((part) => part.length > 1)
    .sort((left, right) => right.length - left.length);

  const lowerValue = value.toLowerCase();
  for (const candidate of candidates) {
    const lowerCandidate = candidate.toLowerCase();
    const index = lowerValue.indexOf(lowerCandidate);
    if (index === -1) {
      continue;
    }

    ranges.push({ start: index, end: index + candidate.length });
  }

  ranges.sort((left, right) => left.start - right.start || right.end - left.end);

  const merged: Array<{ start: number; end: number }> = [];
  for (const range of ranges) {
    const previous = merged[merged.length - 1];
    if (!previous || range.start > previous.end) {
      merged.push(range);
      continue;
    }

    previous.end = Math.max(previous.end, range.end);
  }

  return merged;
}

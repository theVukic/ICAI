import Quill from "quill";
import "quill/dist/quill.snow.css";
import { navigateWithFade } from "./transition";
import { renderPublishSessionBadge } from "./publish-session";
import { loadSessionInfo, renderSessionBadge } from "./session";

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

type DescriptionResponse = {
  profileName: string;
  content: string;
  updatedAt?: string | null;
};

type ErrorResponse = {
  error: string;
};

const state: {
  isLoading: boolean;
  isSaving: boolean;
  message: string;
  tone: "muted" | "danger" | "success";
  profileName: string;
  editorHtml: string;
  quill: Quill | null;
  sessionUsername: string;
  tokenConfigured: boolean;
} = {
  isLoading: false,
  isSaving: false,
  message: "",
  tone: "muted",
  profileName: "",
  editorHtml: "",
  quill: null,
  sessionUsername: "",
  tokenConfigured: false,
};

export function mountDescriptionPage(root: HTMLDivElement): void {
  if (!state.isLoading && state.profileName === "") {
    void loadDescription(root);
  }

  renderDescriptionPage(root);
}

async function loadDescription(root: HTMLDivElement): Promise<void> {
  state.isLoading = true;
  state.message = "";
  renderDescriptionPage(root);

  try {
    const session = await loadSessionInfo();
    state.sessionUsername = session.username || "";
    state.tokenConfigured = session.tokenConfigured;

    const response = await fetch("/api/description");
    const data = (await response.json()) as DescriptionResponse | ErrorResponse;
    if (!response.ok) {
      throw new Error("error" in data ? data.error : "Ucitavanje nije uspelo.");
    }

    const document = data as DescriptionResponse;
    state.profileName = document.profileName || "";
    state.editorHtml = document.content || "";
    state.tone = "muted";
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Ucitavanje nije uspelo.";
    state.tone = "danger";
  } finally {
    state.isLoading = false;
    renderDescriptionPage(root);
  }
}

function renderDescriptionPage(root: HTMLDivElement): void {
  root.innerHTML = `
    <div class="shell edit-shell">
      <header class="topbar edit-topbar">
        <div class="logo-wrap">
          <div class="logo-mark">IC</div>
          <strong>ICAI</strong>
          ${renderSessionBadge(state.tokenConfigured ? { tokenConfigured: true, username: state.sessionUsername, fullName: "" } : null)}
          ${renderPublishSessionBadge()}
        </div>
      </header>

      <main class="edit-page description-page">
        <section class="description-card-shell">
          <article class="description-card">
            <div class="description-card-head">
              <div class="edit-copy">
                <p class="edit-eyebrow">DESKRIPCIJA</p>
                <p class="edit-subtitle">Deskripcija za profil <strong>${escapeHtml(state.profileName || "default")}</strong>.</p>
              </div>
              <div class="description-placeholder-note">
                <strong>Placeholderi:</strong>
                <span>${escapeHtml("${index}")}, ${escapeHtml("${technicalDescription}")}, ${escapeHtml("${nameB2B}")}, ${escapeHtml("${brandName}")}, ${escapeHtml("${retailPrice}")}, ${escapeHtml("${towKod}")}</span>
              </div>
            </div>

            ${
              state.isLoading
                ? `<div class="edit-state">Ucitavam deskripciju...</div>`
                : `
                  <form class="description-form" id="descriptionForm">
                    <div class="description-editor-wrap">
                      <div id="descriptionEditor" class="description-editor"></div>
                    </div>

                    ${
                      state.message
                        ? `<div class="edit-state ${state.tone === "danger" ? "danger" : state.tone === "success" ? "success" : ""}">${escapeHtml(state.message)}</div>`
                        : ""
                    }

                    <div class="edit-card-footer">
                      <button class="ghost-button edit-action-button edit-back-button" type="button" id="descriptionBackButton">
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
            }
          </article>
        </section>
      </main>
    </div>
  `;

  bindDescriptionEvents(root);
  if (!state.isLoading) {
    mountQuillEditor();
  }
}

function bindDescriptionEvents(root: HTMLDivElement): void {
  const backButton =
    document.querySelector<HTMLButtonElement>("#descriptionBackButton");
  const form = document.querySelector<HTMLFormElement>("#descriptionForm");

  backButton?.addEventListener("click", () => {
    navigateWithFade(root, "/");
  });

  form?.addEventListener("submit", async (event) => {
    event.preventDefault();

    state.editorHtml = getEditorHtml();
    state.isSaving = true;
    state.message = "";
    renderDescriptionPage(root);

    try {
      const response = await fetch("/api/description", {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          content: state.editorHtml,
        }),
      });

      const data = (await response.json()) as DescriptionResponse | ErrorResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "Cuvanje nije uspelo.");
      }

      const document = data as DescriptionResponse;
      state.profileName = document.profileName || state.profileName;
      state.editorHtml = document.content || "";
      navigateWithFade(root, "/");
      return;
    } catch (error) {
      state.message =
        error instanceof Error ? error.message : "Cuvanje nije uspelo.";
      state.tone = "danger";
    } finally {
      state.isSaving = false;
      renderDescriptionPage(root);
    }
  });
}

function mountQuillEditor(): void {
  const container = document.querySelector<HTMLDivElement>("#descriptionEditor");
  if (!container) {
    return;
  }

  const quill = new Quill(container, {
    theme: "snow",
    modules: {
      toolbar: [
        [{ header: [1, 2, 3, false] }],
        ["bold", "italic", "underline", "strike"],
        [{ list: "ordered" }, { list: "bullet" }],
        [{ align: [] }],
        ["link", "blockquote"],
        ["clean"],
      ],
    },
    placeholder: "Unesi deskripciju...",
  });

  if (state.editorHtml) {
    quill.clipboard.dangerouslyPasteHTML(state.editorHtml);
  }

  quill.on("text-change", () => {
    state.editorHtml = normalizeEditorHtml(quill.root.innerHTML);
  });

  state.quill = quill;
}

function getEditorHtml(): string {
  if (!state.quill) {
    return normalizeEditorHtml(state.editorHtml);
  }

  return normalizeEditorHtml(state.quill.root.innerHTML);
}

function normalizeEditorHtml(value: string): string {
  const normalized = value.trim();
  return normalized === "<p><br></p>" ? "" : normalized;
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

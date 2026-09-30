export type ManagedImage = {
  id: string;
  position: number;
  url: string;
  source?: string;
};

export type ImageManagerItem = {
  itemId: string;
  nameB2B: string;
  images: ManagedImage[];
};

type PendingImage = {
  key: string;
  file?: File;
  preview: string;
  source: string;
};

type BulkImageResponse<T> = {
  state: T;
  updated: number;
  skipped: number;
};

const allowedImageTypes = new Set(["image/jpeg", "image/png", "image/webp"]);

export function openImageManager(
  item: ImageManagerItem,
  onSaved: (item: ImageManagerItem) => void,
  initialAction: "edit" | "append" | "cover" = "edit",
): void {
  document.querySelector(".image-manager-backdrop")?.remove();
  let newFileIndex = 0;
  let entries: PendingImage[] = [...item.images]
    .sort((left, right) => left.position - right.position)
    .map((image) => ({
      key: image.id,
      preview: image.url,
      source: image.source || "supplier",
    }));

  const backdrop = document.createElement("div");
  backdrop.className = "image-manager-backdrop";
  backdrop.innerHTML = `
    <section class="image-manager-dialog" role="dialog" aria-modal="true" aria-labelledby="imageManagerTitle">
      <header class="image-manager-head">
        <div>
          <span>GALERIJA ARTIKLA</span>
          <h2 id="imageManagerTitle">${escapeHtml(item.nameB2B || "Artikal")}</h2>
          <p>Prevuci slike da promijeniš redoslijed. Prva slika je naslovna.</p>
        </div>
        <button class="image-manager-close" type="button" aria-label="Zatvori">×</button>
      </header>
      <div class="image-manager-toolbar">
        <button class="ghost-button" type="button" data-gallery-add="append">+ Dodaj slike</button>
        <button class="ghost-button" type="button" data-gallery-add="cover">+ Dodaj naslovnu sliku</button>
      </div>
      <div class="image-manager-grid"></div>
      <p class="image-manager-empty" hidden>Artikal trenutno nema nijednu sliku.</p>
      <p class="image-manager-message" aria-live="polite"></p>
      <footer class="image-manager-footer">
        <button class="ghost-button" type="button" data-gallery-cancel>Odustani</button>
        <button class="save-button" type="button" data-gallery-save>Sačuvaj</button>
      </footer>
      <input type="file" data-gallery-picker accept="image/jpeg,image/png,image/webp" multiple hidden />
    </section>
  `;
  document.body.append(backdrop);

  const grid = backdrop.querySelector<HTMLDivElement>(".image-manager-grid")!;
  const empty = backdrop.querySelector<HTMLParagraphElement>(".image-manager-empty")!;
  const message = backdrop.querySelector<HTMLParagraphElement>(".image-manager-message")!;
  const picker = backdrop.querySelector<HTMLInputElement>("[data-gallery-picker]")!;
  const saveButton = backdrop.querySelector<HTMLButtonElement>("[data-gallery-save]")!;
  let pickerMode: "append" | "cover" = "append";
  let closed = false;
  let saving = false;

  const close = () => {
    if (closed || saving) return;
    closed = true;
    for (const entry of entries) {
      if (entry.file && entry.preview.startsWith("blob:")) URL.revokeObjectURL(entry.preview);
    }
    backdrop.remove();
  };

  const renderEntries = () => {
    grid.innerHTML = entries.map((entry, index) => `
      <article class="image-manager-tile ${index === 0 ? "is-cover" : ""}" draggable="true" data-gallery-key="${escapeHtml(entry.key)}">
        <img src="${escapeHtml(entry.preview)}" alt="Slika ${index + 1}" draggable="false" />
        <div class="image-manager-badges">
          ${index === 0 ? `<strong>NASLOVNA</strong>` : ""}
          <span>${sourceLabel(entry.source)}</span>
        </div>
        <div class="image-manager-tile-actions">
          ${index > 0 ? `<button type="button" data-gallery-cover="${escapeHtml(entry.key)}">Postavi kao glavnu</button>` : ""}
          <button class="danger" type="button" data-gallery-delete="${escapeHtml(entry.key)}">Obriši</button>
        </div>
        <div class="image-manager-move-actions">
          <button type="button" data-gallery-move="left" data-gallery-key="${escapeHtml(entry.key)}" ${index === 0 ? "disabled" : ""}>←</button>
          <span>${index + 1}</span>
          <button type="button" data-gallery-move="right" data-gallery-key="${escapeHtml(entry.key)}" ${index === entries.length - 1 ? "disabled" : ""}>→</button>
        </div>
      </article>
    `).join("");
    empty.hidden = entries.length > 0;
    bindTiles();
  };

  const moveEntry = (key: string, targetIndex: number) => {
    const index = entries.findIndex((entry) => entry.key === key);
    if (index < 0 || targetIndex < 0 || targetIndex >= entries.length || index === targetIndex) return;
    const [entry] = entries.splice(index, 1);
    entries.splice(targetIndex, 0, entry);
    renderEntries();
  };

  const bindTiles = () => {
    let draggedKey = "";
    for (const tile of grid.querySelectorAll<HTMLElement>("[data-gallery-key]")) {
      tile.addEventListener("dragstart", () => {
        draggedKey = tile.dataset.galleryKey || "";
        tile.classList.add("is-dragging");
      });
      tile.addEventListener("dragend", () => tile.classList.remove("is-dragging"));
      tile.addEventListener("dragover", (event) => event.preventDefault());
      tile.addEventListener("drop", (event) => {
        event.preventDefault();
        const targetKey = tile.dataset.galleryKey || "";
        moveEntry(draggedKey, entries.findIndex((entry) => entry.key === targetKey));
      });
    }
    for (const button of grid.querySelectorAll<HTMLButtonElement>("[data-gallery-cover]")) {
      button.addEventListener("click", () => moveEntry(button.dataset.galleryCover || "", 0));
    }
    for (const button of grid.querySelectorAll<HTMLButtonElement>("[data-gallery-delete]")) {
      button.addEventListener("click", () => {
        const key = button.dataset.galleryDelete || "";
        const finalImage = entries.length === 1;
        const prompt = finalImage
          ? "Ovo je posljednja slika. Artikal će ostati bez naslovne slike. Da li je sigurno želiš obrisati?"
          : "Da li sigurno želiš obrisati sliku?";
        if (!window.confirm(prompt)) return;
        const entry = entries.find((candidate) => candidate.key === key);
        if (entry?.file && entry.preview.startsWith("blob:")) URL.revokeObjectURL(entry.preview);
        entries = entries.filter((candidate) => candidate.key !== key);
        renderEntries();
      });
    }
    for (const button of grid.querySelectorAll<HTMLButtonElement>("[data-gallery-move]")) {
      button.addEventListener("click", () => {
        const index = entries.findIndex((entry) => entry.key === button.dataset.galleryKey);
        moveEntry(button.dataset.galleryKey || "", index + (button.dataset.galleryMove === "left" ? -1 : 1));
      });
    }
  };

  const openPicker = (mode: "append" | "cover") => {
    pickerMode = mode;
    picker.multiple = mode === "append";
    picker.value = "";
    picker.click();
  };

  picker.addEventListener("change", async () => {
    const selected = Array.from(picker.files || []);
    if (selected.length === 0) return;
    message.textContent = "Pripremam slike...";
    try {
      const processed = await Promise.all(selected.map(resizeImageFile));
      if (closed) return;
      const pending = processed.map((file) => {
        const index = newFileIndex++;
        return {
          key: `new:${index}`,
          file,
          preview: URL.createObjectURL(file),
          source: "manual",
        } satisfies PendingImage;
      });
      entries = pickerMode === "cover" ? [...pending, ...entries] : [...entries, ...pending];
      message.textContent = "";
      renderEntries();
    } catch (error) {
      if (closed) return;
      message.textContent = error instanceof Error ? error.message : "Slike nisu pripremljene.";
    }
  });

  backdrop.querySelector(".image-manager-close")?.addEventListener("click", close);
  backdrop.querySelector("[data-gallery-cancel]")?.addEventListener("click", close);
  backdrop.addEventListener("click", (event) => {
    if (event.target === backdrop) close();
  });
  for (const button of backdrop.querySelectorAll<HTMLButtonElement>("[data-gallery-add]")) {
    button.addEventListener("click", () => openPicker(button.dataset.galleryAdd === "cover" ? "cover" : "append"));
  }
  saveButton.addEventListener("click", async () => {
    saving = true;
    saveButton.disabled = true;
    message.textContent = "Čuvam galeriju...";
    try {
      const files = entries.filter((entry) => entry.file);
      const form = new FormData();
      form.append("itemId", item.itemId);
      form.append("knownIds", JSON.stringify(item.images.map((image) => image.id)));
      for (const entry of files) form.append("files", entry.file!);

      // Tokens sent to the backend refer to the order in the multipart file list,
      // not to the temporary browser counter (which can contain deleted gaps).
      const fileToken = new Map(files.map((entry, index) => [entry.key, `new:${index}`]));
      form.set("order", JSON.stringify(entries.map((entry) => fileToken.get(entry.key) || entry.key)));
      const response = await fetch("/api/catalog/images/save", { method: "POST", body: form });
      const data = await response.json() as ImageManagerItem | { error: string };
      if (!response.ok) throw new Error("error" in data ? data.error : "Galerija nije sačuvana.");
      onSaved(data as ImageManagerItem);
      if ((data as ImageManagerItem).images.length < entries.length) {
        window.alert("Identična slika je već postojala i nije ponovo dodana.");
      }
      saving = false;
      close();
    } catch (error) {
      message.textContent = error instanceof Error ? error.message : "Galerija nije sačuvana.";
      saving = false;
      saveButton.disabled = false;
    }
  });

  renderEntries();
  if (initialAction !== "edit") openPicker(initialAction);
}

export function pickAndUploadBulkImages<T>(
  itemKeys: string[],
  mode: "append" | "cover",
): Promise<BulkImageResponse<T> | null> {
  return new Promise((resolve, reject) => {
    let settled = false;
    const picker = document.createElement("input");
    picker.type = "file";
    picker.accept = "image/jpeg,image/png,image/webp";
    picker.multiple = mode === "append";
    picker.hidden = true;
    document.body.append(picker);
    const cleanup = () => picker.remove();
    const finish = (value: BulkImageResponse<T> | null) => {
      if (settled) return;
      settled = true;
      cleanup();
      resolve(value);
    };
    picker.addEventListener("cancel", () => finish(null), { once: true });
    picker.addEventListener("change", async () => {
      const selected = Array.from(picker.files || []);
      if (selected.length === 0) {
        finish(null);
        return;
      }
      try {
        const files = await Promise.all(selected.map(resizeImageFile));
        const form = new FormData();
        form.append("itemKeys", JSON.stringify(itemKeys));
        form.append("mode", mode);
        for (const file of files) form.append("files", file);
        const response = await fetch("/api/catalog/images/bulk", { method: "POST", body: form });
        const data = await response.json() as BulkImageResponse<T> | { error: string };
        if (!response.ok) throw new Error("error" in data ? data.error : "Slike nisu dodane.");
        finish(data as BulkImageResponse<T>);
      } catch (error) {
		settled = true;
		cleanup();
		reject(error);
      }
    }, { once: true });
    picker.click();
  });
}

async function resizeImageFile(file: File): Promise<File> {
  if (!allowedImageTypes.has(file.type)) {
    throw new Error(`${file.name} nije JPG, PNG ili WebP slika.`);
  }
  const bitmap = await createImageBitmap(file);
  const scale = Math.min(1, 2000 / bitmap.width, 2000 / bitmap.height);
  if (scale === 1) {
    bitmap.close();
    return file;
  }
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(bitmap.width * scale));
  canvas.height = Math.max(1, Math.round(bitmap.height * scale));
  const context = canvas.getContext("2d");
  if (!context) {
    bitmap.close();
    throw new Error(`Slika ${file.name} nije mogla biti smanjena.`);
  }
  context.imageSmoothingEnabled = true;
  context.imageSmoothingQuality = "high";
  context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  bitmap.close();
  const blob = await new Promise<Blob>((resolve, reject) => {
    canvas.toBlob((result) => result ? resolve(result) : reject(new Error(`Slika ${file.name} nije mogla biti smanjena.`)), file.type, 0.92);
  });
  return new File([blob], file.name, { type: file.type, lastModified: Date.now() });
}

function sourceLabel(source: string): string {
  if (source === "ai") return "AI";
  if (source === "manual") return "RUČNA";
  return "DOBAVLJAČ";
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

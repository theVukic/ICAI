type SuggestionResponse = {
  suggestions?: string[];
};

const SEARCH_DELAY_MS = 120;

export function bindTitleAutocomplete(input: HTMLInputElement | null): void {
  if (!input) return;

  const list = input.parentElement?.querySelector<HTMLDivElement>(".title-suggestions");
  if (!list) return;

  let suggestions: string[] = [];
  let activeIndex = -1;
  let timer = 0;
  let request: AbortController | null = null;

  const close = () => {
    suggestions = [];
    activeIndex = -1;
    list.replaceChildren();
    list.hidden = true;
    input.setAttribute("aria-expanded", "false");
    input.removeAttribute("aria-activedescendant");
  };

  const select = (index: number) => {
    const value = suggestions[index];
    if (!value) return;
    input.value = value;
    close();
    input.focus();
  };

  const remove = async (value: string, button: HTMLButtonElement) => {
    button.disabled = true;
    try {
      const response = await fetch(
        `/api/title-suggestions?title=${encodeURIComponent(value)}`,
        { method: "DELETE" },
      );
      if (!response.ok) {
        button.disabled = false;
        return;
      }
      render(suggestions.filter((suggestion) => suggestion !== value));
      input.focus();
    } catch {
      button.disabled = false;
    }
  };

  const setActive = (index: number) => {
    const options = Array.from(list.querySelectorAll<HTMLButtonElement>(".title-suggestion"));
    if (options.length === 0) return;
    activeIndex = (index + options.length) % options.length;
    options.forEach((option, optionIndex) => {
      const active = optionIndex === activeIndex;
      option.classList.toggle("is-active", active);
      option.setAttribute("aria-selected", String(active));
    });
    const activeOption = options[activeIndex];
    input.setAttribute("aria-activedescendant", activeOption.id);
    activeOption.scrollIntoView({ block: "nearest" });
  };

  const render = (values: string[]) => {
    suggestions = values.filter((value) => value.trim() !== "" && value !== input.value);
    activeIndex = -1;
    list.replaceChildren();
    if (suggestions.length === 0 || document.activeElement !== input) {
      close();
      return;
    }

    suggestions.forEach((value, index) => {
      const row = document.createElement("div");
      row.className = "title-suggestion-row";

      const option = document.createElement("button");
      option.type = "button";
      option.className = "title-suggestion";
      option.id = `${input.id}-suggestion-${index}`;
      option.setAttribute("role", "option");
      option.setAttribute("aria-selected", "false");
      option.textContent = value;
      option.addEventListener("mousedown", (event) => event.preventDefault());
      option.addEventListener("click", () => select(index));

      const deleteButton = document.createElement("button");
      deleteButton.type = "button";
      deleteButton.className = "title-suggestion-delete";
      deleteButton.setAttribute("aria-label", `Obriši sačuvani naslov: ${value}`);
      deleteButton.title = "Obriši sačuvani naslov";
      deleteButton.textContent = "×";
      deleteButton.addEventListener("mousedown", (event) => event.preventDefault());
      deleteButton.addEventListener("click", () => void remove(value, deleteButton));

      row.append(option, deleteButton);
      list.append(row);
    });
    list.hidden = false;
    input.setAttribute("aria-expanded", "true");
  };

  const search = async () => {
    const query = input.value.trim();
    if (!query) {
      close();
      return;
    }

    request?.abort();
    request = new AbortController();
    try {
      const response = await fetch(`/api/title-suggestions?q=${encodeURIComponent(query)}`, {
        signal: request.signal,
      });
      if (!response.ok) {
        close();
        return;
      }
      const data = (await response.json()) as SuggestionResponse;
      if (query !== input.value.trim()) return;
      render(Array.isArray(data.suggestions) ? data.suggestions : []);
    } catch (error) {
      if (!(error instanceof DOMException && error.name === "AbortError")) close();
    }
  };

  const scheduleSearch = () => {
    window.clearTimeout(timer);
    timer = window.setTimeout(() => void search(), SEARCH_DELAY_MS);
  };

  input.setAttribute("autocomplete", "off");
  input.setAttribute("role", "combobox");
  input.setAttribute("aria-autocomplete", "list");
  input.setAttribute("aria-controls", list.id);
  input.setAttribute("aria-expanded", "false");
  input.addEventListener("input", scheduleSearch);
  input.addEventListener("focus", scheduleSearch);
  input.addEventListener("blur", () => window.setTimeout(close, 100));
  input.addEventListener("keydown", (event) => {
    if (event.key === "ArrowDown" && suggestions.length > 0) {
      event.preventDefault();
      setActive(activeIndex + 1);
    } else if (event.key === "ArrowUp" && suggestions.length > 0) {
      event.preventDefault();
      setActive(activeIndex - 1);
    } else if (event.key === "Enter" && activeIndex >= 0) {
      event.preventDefault();
      select(activeIndex);
    } else if (event.key === "Escape") {
      close();
    }
  });
}

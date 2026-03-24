const TRANSITION_MS = 220;

function emitRouteChange(): void {
  window.dispatchEvent(new PopStateEvent("popstate"));
}

function revealPage(root: HTMLElement): void {
  requestAnimationFrame(() => {
    root.classList.add("page-visible");
  });
}

export function mountPageTransition(root: HTMLElement): void {
  root.classList.add("page-mounted");
  revealPage(root);
}

export function navigateWithFade(root: HTMLElement, url: string): void {
  root.classList.remove("page-visible");
  root.classList.add("page-leaving");

  window.setTimeout(() => {
    window.history.pushState({}, "", url);
    root.classList.remove("page-leaving");
    emitRouteChange();
    revealPage(root);
  }, TRANSITION_MS);
}

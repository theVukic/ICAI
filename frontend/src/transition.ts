const TRANSITION_MS = 220;

export function mountPageTransition(root: HTMLElement): void {
  root.classList.add("page-mounted");

  requestAnimationFrame(() => {
    root.classList.add("page-visible");
  });
}

export function navigateWithFade(root: HTMLElement, url: string): void {
  root.classList.remove("page-visible");
  root.classList.add("page-leaving");

  window.setTimeout(() => {
    window.location.assign(url);
  }, TRANSITION_MS);
}

import { renderAppBrand } from "./branding";

export type LoginScreenState = {
  authSubmitting: boolean;
  authError: string;
  rememberMe: boolean;
  showFieldError: boolean;
};

export type LoginSubmitPayload = {
  username: string;
  password: string;
  rememberMe: boolean;
};

export function renderLoginScreen(
  root: HTMLElement,
  state: LoginScreenState,
  onSubmit: (payload: LoginSubmitPayload) => void | Promise<void>,
): void {
  root.innerHTML = `
    <div class="login-screen">
      <div class="login-glow login-glow-left"></div>
      <div class="login-glow login-glow-right"></div>
      <section class="login-card">
        <div class="login-brand">${renderAppBrand()}</div>
        <div class="login-copy">
          <span class="login-eyebrow">PRISTUP APLIKACIJI</span>
        </div>
        <form class="login-form" id="loginForm">
          <label class="login-field">
            <span>Username</span>
            <input class="${state.showFieldError ? "login-input-error" : ""}" id="loginUsername" type="text" autocomplete="username" placeholder="Unesite username" ${state.authSubmitting ? "disabled" : ""} />
          </label>
          <label class="login-field">
            <span>Password</span>
            <input class="${state.showFieldError ? "login-input-error" : ""}" id="loginPassword" type="password" autocomplete="current-password" placeholder="Unesite password" ${state.authSubmitting ? "disabled" : ""} />
          </label>
          <label class="login-remember">
            <input id="rememberMe" type="checkbox" ${state.rememberMe ? "checked" : ""} ${state.authSubmitting ? "disabled" : ""} />
            <span>Zapamti korisnika</span>
          </label>
          <button class="login-submit" type="submit" ${state.authSubmitting ? "disabled" : ""}>
            ${state.authSubmitting ? "Provjera..." : "Prijavi se"}
          </button>
        </form>
      </section>
    </div>
  `;

  const form = root.querySelector<HTMLFormElement>("#loginForm");
  const usernameInput = root.querySelector<HTMLInputElement>("#loginUsername");
  const passwordInput = root.querySelector<HTMLInputElement>("#loginPassword");
  const rememberMeInput = root.querySelector<HTMLInputElement>("#rememberMe");

  usernameInput?.focus();

  form?.addEventListener("submit", (event) => {
    event.preventDefault();

    onSubmit({
      username: usernameInput?.value.trim() || "",
      password: passwordInput?.value || "",
      rememberMe: rememberMeInput?.checked ?? false,
    });
  });
}

export function renderAuthLoading(root: HTMLElement): void {
  root.innerHTML = `
    <div class="login-screen login-screen-loading">
      <section class="login-card login-card-loading">
        <div class="login-brand">${renderAppBrand()}</div>
        <div class="login-copy">
          <span class="login-eyebrow">SIGURNA PROVJERA</span>
          <h1>Provjeravam session.</h1>
          <p>Sačekajte trenutak dok backend potvrdi pristup aplikaciji.</p>
        </div>
      </section>
    </div>
  `;
}

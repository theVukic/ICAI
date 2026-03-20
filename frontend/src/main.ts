import "./style.css";

const app = document.querySelector<HTMLDivElement>("#app");

if (!app) throw new Error("No #app");

app.innerHTML = `
  <div class="container">
    <h1>Test dugme</h1>

    <button id="testBtn">Klikni me</button>

    <p id="result">Klikni dugme...</p>
  </div>
`;

const btn = document.querySelector<HTMLButtonElement>("#testBtn")!;
const result = document.querySelector<HTMLParagraphElement>("#result")!;

btn.addEventListener("click", async () => {
  result.textContent = "Loading...";

  try {
    const res = await fetch("/api/test");
    const data = await res.json();

    result.textContent = data.message;
  } catch (err) {
    result.textContent = "Greška!";
  }
});

try {
  const choice = localStorage.getItem("vgatebox.theme") || "auto";
  const dark = choice === "dark" || (choice !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.style.colorScheme = dark ? "dark" : "light";
} catch {
  // Storage can be unavailable in hardened browsers.
}

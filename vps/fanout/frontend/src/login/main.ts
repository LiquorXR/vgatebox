import { createApp } from "vue";
import LoginPage from "./LoginPage.vue";
import { applyTheme } from "@/lib/theme";
import "@/styles/main.css";

applyTheme();
createApp(LoginPage).mount("#login-app");

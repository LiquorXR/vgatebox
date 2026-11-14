import { VueQueryPlugin, QueryClient } from "@tanstack/vue-query";
import { createApp } from "vue";
import App from "./App.vue";
import router from "./router";
import { applyTheme } from "./lib/theme";
import "./styles/main.css";

applyTheme();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: true,
      staleTime: 1500,
    },
  },
});

createApp(App).use(router).use(VueQueryPlugin, { queryClient }).mount("#app");

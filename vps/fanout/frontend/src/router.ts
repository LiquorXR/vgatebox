import { createRouter, createWebHashHistory } from "vue-router";

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: "/",
      name: "overview",
      component: () => import("@/pages/OverviewPage.vue"),
      meta: { title: "总览" },
    },
    {
      path: "/exits",
      name: "exits",
      component: () => import("@/pages/ExitsPage.vue"),
      meta: { title: "出口" },
    },
    {
      path: "/nodes",
      name: "nodes",
      component: () => import("@/pages/NodesPage.vue"),
      meta: { title: "节点池" },
    },
    {
      path: "/subscription",
      name: "subscription",
      component: () => import("@/pages/SubscriptionPage.vue"),
      meta: { title: "订阅" },
    },
    {
      path: "/access",
      name: "access",
      component: () => import("@/pages/AccessPage.vue"),
      meta: { title: "接入" },
    },
    {
      path: "/settings",
      name: "settings",
      component: () => import("@/pages/SettingsPage.vue"),
      meta: { title: "设置" },
    },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
  scrollBehavior() {
    return { top: 0 };
  },
});

router.afterEach((to) => {
  const title = typeof to.meta.title === "string" ? to.meta.title : "控制台";
  document.title = `vgatebox · ${title}`;
});

export default router;

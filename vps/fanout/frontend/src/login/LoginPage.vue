<script setup lang="ts">
import { ArrowRight, Eye, EyeOff, LockKeyhole, Moon, ShieldCheck, Sun } from "@lucide/vue";
import { ref } from "vue";
import { isDark, toggleTheme } from "@/lib/theme";

const password = ref("");
const visible = ref(false);
const busy = ref(false);
const error = ref("");

async function submit() {
  if (!password.value || busy.value) return;
  busy.value = true;
  error.value = "";
  try {
    const response = await fetch("login", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        Accept: "application/json",
      },
      body: new URLSearchParams({ password: password.value }),
    });
    const data = (await response.json().catch(() => ({}))) as { error?: string; ok?: string };
    if (!response.ok) {
      error.value = data.error || (response.status === 429 ? "登录失败次数过多，请稍后再试" : "登录失败，请检查口令");
      return;
    }
    window.location.reload();
  } catch {
    error.value = "无法连接到管理服务，请检查网络后重试";
  } finally {
    busy.value = false;
  }
}

const steps = [
  { label: "来源", value: "候选清单", tone: "ok" },
  { label: "出口", value: "按策略连接", tone: "info" },
  { label: "订阅", value: "一条链接", tone: "accent" },
  { label: "客户端", value: "随时更新", tone: "neutral" },
];
</script>

<template>
  <div class="grid min-h-dvh bg-canvas text-ink lg:grid-cols-[1.08fr_1fr]">
    <section class="relative hidden overflow-hidden bg-[#0b1a1d] px-12 py-10 text-white lg:flex lg:flex-col">
      <div
        class="pointer-events-none absolute inset-0 opacity-[0.16]"
        style="background-image: linear-gradient(rgba(255,255,255,.14) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,.14) 1px, transparent 1px); background-size: 56px 56px;"
        aria-hidden="true"
      />
      <div class="relative flex items-center gap-2.5">
        <span class="grid size-9 place-items-center rounded-lg bg-[#5fd4c4] text-[#062622]" aria-hidden="true">
          <svg viewBox="0 0 16 16" fill="none" class="size-4.5">
            <path d="M3 4.2 8 8l5-3.8M3 8.4 8 12.2l5-3.8" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </span>
        <div>
          <div class="text-[15px] font-semibold tracking-tight">vgatebox</div>
          <div class="text-[10px] uppercase tracking-[0.18em] text-white/40">Independent exits</div>
        </div>
      </div>

      <div class="relative my-auto max-w-md">
        <p class="text-[11px] uppercase tracking-[0.2em] text-[#5fd4c4]">Control your own network</p>
        <h1 class="mt-4 text-[38px] font-semibold leading-[1.15] tracking-tight">
          每一条连接，<br />都有清晰的去向。
        </h1>
        <p class="mt-5 max-w-sm text-sm leading-relaxed text-white/60">
          在自己的 VPS 上管理 VPN Gate、第三方 SOCKS5 和本机直出，用一条订阅连接所有出口。
        </p>

        <div class="mt-10 grid grid-cols-2 gap-2.5">
          <div v-for="step in steps" :key="step.label" class="rounded-lg border border-white/10 bg-white/[0.04] px-3 py-2.5">
            <div class="flex items-center gap-1.5 text-[10px] uppercase tracking-[0.12em] text-white/35">
              <span
                class="size-1.5 rounded-full"
                :class="{
                  'bg-[#5fd4c4]': step.tone === 'accent',
                  'bg-[#6ee7b7]': step.tone === 'ok',
                  'bg-[#7dd3fc]': step.tone === 'info',
                  'bg-white/40': step.tone === 'neutral',
                }"
              />
              {{ step.label }}
            </div>
            <div class="mt-1 text-xs font-medium text-white/80">{{ step.value }}</div>
          </div>
        </div>
      </div>

      <div class="relative flex items-center gap-2 text-[11px] text-white/35">
        <ShieldCheck :size="14" />
        独立运行 · 单二进制 · 无外部依赖
      </div>
    </section>

    <section class="flex items-center justify-center px-5 py-10 sm:px-8">
      <div class="w-full max-w-sm">
        <div class="mb-10 flex items-center justify-between lg:justify-end">
          <div class="flex items-center gap-2 lg:hidden">
            <span class="grid size-8 place-items-center rounded-lg bg-accent text-accent-ink" aria-hidden="true">
              <svg viewBox="0 0 16 16" fill="none" class="size-4">
                <path d="M3 4.2 8 8l5-3.8M3 8.4 8 12.2l5-3.8" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </span>
            <span class="text-sm font-semibold">vgatebox</span>
          </div>
          <button
            type="button"
            class="grid size-9 place-items-center rounded-lg border border-line text-muted hover:bg-surface-2 hover:text-ink"
            :aria-label="isDark ? '切换到浅色主题' : '切换到深色主题'"
            @click="toggleTheme"
          >
            <Sun v-if="isDark" :size="16" />
            <Moon v-else :size="16" />
          </button>
        </div>

        <div class="mb-7">
          <div class="mb-2 flex items-center gap-2 text-[11px] text-muted">
            <span class="size-1.5 rounded-full bg-ok" aria-hidden="true" />管理服务在线
          </div>
          <h2 class="text-2xl font-semibold tracking-tight">登录控制台</h2>
          <p class="mt-1.5 text-sm text-muted">使用安装完成时显示的访问口令。</p>
        </div>

        <form method="post" action="login" class="space-y-4" @submit.prevent="submit">
          <input type="text" name="username" value="vgatebox" autocomplete="username" tabindex="-1" aria-hidden="true" class="sr-only" />
          <label class="block">
            <span class="mb-1.5 block text-xs font-medium text-ink">访问口令</span>
            <span class="relative block">
              <LockKeyhole :size="15" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" />
              <input
                v-model="password"
                :type="visible ? 'text' : 'password'"
                name="password"
                autocomplete="current-password"
                autofocus
                required
                class="h-11 w-full rounded-lg border border-line-strong bg-surface pl-9 pr-11 text-sm outline-none transition-colors focus:border-accent"
                placeholder="输入访问口令"
                aria-describedby="login-error"
              />
              <button
                type="button"
                class="absolute right-1.5 top-1/2 grid size-10 -translate-y-1/2 place-items-center rounded-md text-muted hover:bg-surface-3 hover:text-ink"
                :aria-label="visible ? '隐藏口令' : '显示口令'"
                @click="visible = !visible"
              >
                <EyeOff v-if="visible" :size="15" />
                <Eye v-else :size="15" />
              </button>
            </span>
          </label>

          <p v-if="error" id="login-error" class="rounded-lg border border-bad/25 bg-bad-soft px-3 py-2 text-xs text-bad" role="alert">
            {{ error }}
          </p>

          <button
            type="submit"
            :disabled="busy || !password"
            class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-lg bg-accent px-4 text-sm font-semibold text-accent-ink transition-colors hover:bg-accent-hover disabled:opacity-50"
          >
            {{ busy ? "正在验证…" : "登录" }}
            <ArrowRight v-if="!busy" :size="16" />
          </button>
        </form>

        <div class="mt-8 space-y-2 text-[11px] leading-relaxed text-faint">
          <p>忘记地址时，在 VPS 执行 <code class="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[10px] text-muted">vgatebox access</code>。</p>
          <p>连续输错 8 次会冷却 2 分钟。管理地址和口令含私密信息，请妥善保存。</p>
        </div>
      </div>
    </section>
  </div>
</template>

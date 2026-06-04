<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from 'vue';

const props = defineProps<{
  siteKey: string;
}>();

const emit = defineEmits<{
  verified: [token: string];
}>();

const containerRef = ref<HTMLDivElement>();
const widgetId = ref<string | null>(null);
const scriptLoaded = ref(false);

/** 动态加载 Turnstile SDK */
function loadTurnstileScript(): Promise<void> {
  return new Promise((resolve, reject) => {
    // 如果已加载，直接返回
    if (window.turnstile) {
      scriptLoaded.value = true;
      resolve();
      return;
    }

    const existingScript = document.querySelector(
      'script[src="https://challenges.cloudflare.com/turnstile/v0/api.js"]'
    );
    if (existingScript) {
      // 脚本标签已存在，等待加载完成
      existingScript.addEventListener('load', () => {
        scriptLoaded.value = true;
        resolve();
      });
      existingScript.addEventListener('error', () => {
        reject(new Error('Failed to load Turnstile script'));
      });
      return;
    }

    const script = document.createElement('script');
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js';
    script.async = true;
    script.defer = true;
    script.onload = () => {
      scriptLoaded.value = true;
      resolve();
    };
    script.onerror = () => {
      reject(new Error('Failed to load Turnstile script'));
    };
    document.head.appendChild(script);
  });
}

/** 渲染 Turnstile widget */
function renderWidget() {
  if (!window.turnstile || !containerRef.value || !props.siteKey) return;

  // 清除旧 widget
  if (widgetId.value !== null) {
    window.turnstile.remove(widgetId.value);
    widgetId.value = null;
  }

  widgetId.value = window.turnstile.render(containerRef.value, {
    sitekey: props.siteKey,
    callback: (token: string) => {
      emit('verified', token);
    },
  });
}

/** 重置 widget（供父组件调用） */
function reset() {
  if (widgetId.value !== null && window.turnstile) {
    window.turnstile.reset(widgetId.value);
  }
}

onMounted(async () => {
  try {
    await loadTurnstileScript();
    renderWidget();
  } catch (e) {
    console.error('[TurnstileWidget] Failed to load Turnstile SDK:', e);
  }
});

onBeforeUnmount(() => {
  if (widgetId.value !== null && window.turnstile) {
    window.turnstile.remove(widgetId.value);
    widgetId.value = null;
  }
});

// 当 siteKey 变化时重新渲染
watch(
  () => props.siteKey,
  () => {
    if (scriptLoaded.value) {
      renderWidget();
    }
  }
);

defineExpose({ reset });
</script>

<template>
  <div ref="containerRef" class="turnstile-container" />
</template>

<style scoped>
.turnstile-container {
  display: flex;
  justify-content: center;
  margin: 12px 0;
}
</style>

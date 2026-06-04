<script setup lang="ts">
import { ref, computed, watchEffect, nextTick } from 'vue';
import { NButton, NTooltip, useMessage } from 'naive-ui';
import {
  CopyOutline,
  EyeOutline,
  EyeOffOutline,
  ReorderFourOutline
} from '@vicons/ionicons5';
import { NIcon } from 'naive-ui';

export interface CodeBlockProps {
  /** 要展示的文本内容 */
  content: string;
  /** 语言标识 */
  language?: 'shell' | 'json' | 'yaml' | 'nginx' | 'text';
  /** 最大高度，如 '300px' */
  maxHeight?: string;
  /** 是否自动换行（默认 false，横向滚动） */
  wrap?: boolean;
  /** 是否为敏感内容（默认 false） */
  sensitive?: boolean;
  /** 是否显示复制按钮（默认 true） */
  showCopy?: boolean;
}

const props = withDefaults(defineProps<CodeBlockProps>(), {
  language: 'text',
  maxHeight: undefined,
  wrap: false,
  sensitive: false,
  showCopy: true
});

const message = useMessage();

/** 敏感内容是否已揭示 */
const revealed = ref(false);

/** 当前是否自动换行 */
const isWrapped = ref(props.wrap);

/** code 元素引用 */
const codeRef = ref<HTMLElement | null>(null);

/** 显示的内容：敏感模式下遮罩，否则原始内容 */
const displayContent = computed(() => {
  if (props.sensitive && !revealed.value) {
    return '••••••••';
  }
  return props.content;
});

/** 容器样式 */
const containerStyle = computed(() => {
  const style: Record<string, string> = {};
  if (props.maxHeight) {
    style.maxHeight = props.maxHeight;
    style.overflowY = 'auto';
  }
  return style;
});

/** pre 元素的 white-space 样式 */
const preStyle = computed(() => ({
  whiteSpace: isWrapped.value ? 'pre-wrap' : 'pre',
  wordBreak: isWrapped.value ? 'break-all' as const : 'normal' as const,
  overflowX: isWrapped.value ? 'hidden' as const : 'auto' as const,
  margin: '0'
}));

/**
 * 安全渲染：使用 textContent 设置内容
 * 绝不使用 v-html 或 innerHTML（Codex 约束 #5）
 */
watchEffect(() => {
  void nextTick(() => {
    if (codeRef.value) {
      codeRef.value.textContent = displayContent.value;
    }
  });
});

/** 复制原始内容到剪贴板 */
async function handleCopy() {
  try {
    await navigator.clipboard.writeText(props.content);
    message.success('已复制到剪贴板');
  } catch {
    message.error('复制失败');
  }
}

/** 切换敏感内容显示/隐藏 */
function toggleReveal() {
  revealed.value = !revealed.value;
}

/** 切换换行模式 */
function toggleWrap() {
  isWrapped.value = !isWrapped.value;
}
</script>

<template>
  <div class="code-block">
    <!-- 工具栏 -->
    <div class="code-block__toolbar">
      <span v-if="language !== 'text'" class="code-block__lang">{{ language }}</span>
      <div class="code-block__actions">
        <!-- 换行切换 -->
        <NTooltip trigger="hover">
          <template #trigger>
            <NButton quaternary size="tiny" @click="toggleWrap">
              <template #icon>
                <NIcon>
                  <ReorderFourOutline />
                </NIcon>
              </template>
            </NButton>
          </template>
          {{ isWrapped ? '横向滚动' : '自动换行' }}
        </NTooltip>

        <!-- 敏感内容切换 -->
        <NTooltip v-if="sensitive" trigger="hover">
          <template #trigger>
            <NButton quaternary size="tiny" @click="toggleReveal">
              <template #icon>
                <NIcon>
                  <EyeOutline v-if="!revealed" />
                  <EyeOffOutline v-else />
                </NIcon>
              </template>
            </NButton>
          </template>
          {{ revealed ? '隐藏内容' : '显示内容' }}
        </NTooltip>

        <!-- 复制按钮 -->
        <NTooltip v-if="showCopy" trigger="hover">
          <template #trigger>
            <NButton quaternary size="tiny" @click="handleCopy">
              <template #icon>
                <NIcon>
                  <CopyOutline />
                </NIcon>
              </template>
            </NButton>
          </template>
          复制
        </NTooltip>
      </div>
    </div>

    <!-- 代码内容区域 -->
    <div class="code-block__content" :style="containerStyle">
      <pre :style="preStyle" :class="[`language-${language}`]"><code ref="codeRef" class="code-block__code"></code></pre>
    </div>
  </div>
</template>

<style scoped>
.code-block {
  border: 1px solid var(--n-border-color, #e0e0e6);
  border-radius: 6px;
  overflow: hidden;
  background-color: var(--n-color, #fafafc);
  font-size: 13px;
}

.code-block__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px;
  background-color: var(--n-color-embedded, #f3f3f5);
  border-bottom: 1px solid var(--n-border-color, #e0e0e6);
  min-height: 32px;
}

.code-block__lang {
  font-size: 11px;
  color: var(--n-text-color-3, #999);
  text-transform: uppercase;
  font-weight: 500;
  letter-spacing: 0.5px;
}

.code-block__actions {
  display: flex;
  align-items: center;
  gap: 2px;
}

.code-block__content {
  padding: 12px 16px;
  overflow-x: auto;
}

.code-block__code {
  font-family: 'Fira Code', 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  color: var(--n-text-color, #333);
}
</style>

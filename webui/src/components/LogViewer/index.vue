<script setup lang="ts">
import { ref, watch, nextTick, computed } from 'vue';
import { NInput, NButton, NEmpty, NSpin } from 'naive-ui';
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5';
import { NIcon } from 'naive-ui';

interface Props {
  logs: string[];
  maxHeight?: string;
  autoScroll?: boolean;
  loading?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  maxHeight: '400px',
  autoScroll: true,
  loading: false,
});

const emit = defineEmits<{
  (e: 'refresh'): void;
}>();

const searchKeyword = ref('');
const containerRef = ref<HTMLElement | null>(null);

/** Determine log level class from line content */
function getLogLevelClass(line: string): string {
  if (/\[ERROR\]/i.test(line)) return 'log-error';
  if (/\[WARN\]/i.test(line)) return 'log-warn';
  if (/\[INFO\]/i.test(line)) return 'log-info';
  return '';
}

interface TextSegment {
  text: string;
  isMatch: boolean;
}

/** Split a line into segments based on search keyword for safe highlighting */
function getHighlightedSegments(line: string): TextSegment[] {
  const keyword = searchKeyword.value.trim();
  if (!keyword) {
    return [{ text: line, isMatch: false }];
  }

  const segments: TextSegment[] = [];
  const lowerLine = line.toLowerCase();
  const lowerKeyword = keyword.toLowerCase();
  let lastIndex = 0;

  let searchStart = 0;
  while (searchStart < lowerLine.length) {
    const matchIndex = lowerLine.indexOf(lowerKeyword, searchStart);
    if (matchIndex === -1) break;

    // Add non-matching segment before this match
    if (matchIndex > lastIndex) {
      segments.push({ text: line.slice(lastIndex, matchIndex), isMatch: false });
    }

    // Add matching segment (use original case from line)
    segments.push({ text: line.slice(matchIndex, matchIndex + keyword.length), isMatch: true });

    lastIndex = matchIndex + keyword.length;
    searchStart = lastIndex;
  }

  // Add remaining non-matching segment
  if (lastIndex < line.length) {
    segments.push({ text: line.slice(lastIndex), isMatch: false });
  }

  // If no segments were created (shouldn't happen, but safety)
  if (segments.length === 0) {
    segments.push({ text: line, isMatch: false });
  }

  return segments;
}

/** Check if a line contains the search keyword */
function lineMatchesSearch(line: string): boolean {
  const keyword = searchKeyword.value.trim();
  if (!keyword) return true;
  return line.toLowerCase().includes(keyword.toLowerCase());
}

/** Filtered and visible log lines (show all, but highlight matches) */
const visibleLogs = computed(() => props.logs);

/** Scroll container to bottom */
function scrollToBottom() {
  if (!containerRef.value) return;
  containerRef.value.scrollTop = containerRef.value.scrollHeight;
}

/** Watch logs changes for auto-scroll */
watch(
  () => props.logs,
  () => {
    if (props.autoScroll) {
      nextTick(() => {
        scrollToBottom();
      });
    }
  },
  { deep: true }
);

function handleRefresh() {
  emit('refresh');
}
</script>

<template>
  <div class="log-viewer">
    <!-- Toolbar -->
    <div class="log-viewer-toolbar">
      <NInput
        v-model:value="searchKeyword"
        placeholder="搜索日志..."
        clearable
        size="small"
        class="log-search-input"
      >
        <template #prefix>
          <NIcon :component="SearchOutline" />
        </template>
      </NInput>
      <NButton size="small" quaternary @click="handleRefresh">
        <template #icon>
          <NIcon :component="RefreshOutline" />
        </template>
        刷新
      </NButton>
    </div>

    <!-- Log container -->
    <div class="log-viewer-container-wrapper">
      <!-- Loading overlay -->
      <div v-if="loading" class="log-viewer-loading">
        <NSpin size="medium" />
      </div>

      <!-- Empty state -->
      <div v-if="!loading && visibleLogs.length === 0" class="log-viewer-empty">
        <NEmpty description="暂无日志" />
      </div>

      <!-- Log lines -->
      <div
        v-show="visibleLogs.length > 0"
        ref="containerRef"
        class="log-viewer-container"
        :style="{ maxHeight: maxHeight }"
      >
        <div
          v-for="(line, lineIdx) in visibleLogs"
          :key="lineIdx"
          class="log-line"
          :class="[getLogLevelClass(line), { 'log-line-dimmed': !lineMatchesSearch(line) }]"
        >
          <span class="log-line-number">{{ lineIdx + 1 }}</span>
          <span class="log-line-content">
            <template v-for="(segment, segIdx) in getHighlightedSegments(line)" :key="segIdx">
              <mark v-if="segment.isMatch" class="log-highlight">{{ segment.text }}</mark>
              <span v-else>{{ segment.text }}</span>
            </template>
          </span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.log-viewer {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.log-viewer-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
}

.log-search-input {
  max-width: 280px;
}

.log-viewer-container-wrapper {
  position: relative;
  border: 1px solid var(--n-border-color, #e0e0e6);
  border-radius: 4px;
  background-color: #1e1e2e;
}

.log-viewer-loading {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(30, 30, 46, 0.7);
  z-index: 10;
  border-radius: 4px;
}

.log-viewer-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 120px;
  background-color: #fafafa;
  border-radius: 4px;
}

.log-viewer-container {
  overflow-y: auto;
  overflow-x: auto;
  padding: 8px 0;
  font-family: 'Fira Code', 'Consolas', 'Monaco', monospace;
  font-size: 13px;
  line-height: 1.6;
}

.log-line {
  display: flex;
  padding: 1px 12px;
  white-space: pre;
  color: #cdd6f4;
}

.log-line:hover {
  background-color: rgba(255, 255, 255, 0.05);
}

.log-line-number {
  flex-shrink: 0;
  width: 40px;
  text-align: right;
  padding-right: 12px;
  color: #6c7086;
  user-select: none;
}

.log-line-content {
  flex: 1;
  min-width: 0;
}

/* Log level colors */
.log-info {
  color: #a6e3a1;
}

.log-warn {
  color: #f9e2af;
}

.log-error {
  color: #f38ba8;
}

/* Search highlight */
.log-highlight {
  background-color: #f9e2af;
  color: #1e1e2e;
  border-radius: 2px;
  padding: 0 1px;
}

/* Dimmed lines that don't match search */
.log-line-dimmed {
  opacity: 0.35;
}
</style>

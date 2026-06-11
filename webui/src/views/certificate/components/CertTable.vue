<script setup lang="ts">
import { computed, h } from 'vue';
import { NDataTable, NTag, NSpace, NButton, NSwitch, NTooltip, NIcon } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { TrashOutline, CheckmarkCircleOutline, AlertCircleOutline } from '@vicons/ionicons5';
import { formatDateTime, daysUntil } from '@/utils/date';
import { usePermission } from '@/hooks/usePermission';

interface Props {
  data: Api.Certificate[];
  loading: boolean;
  deletingIds?: Set<string>;
}

interface Emits {
  (e: 'delete', row: Api.Certificate): void;
}

const props = withDefaults(defineProps<Props>(), {
  deletingIds: () => new Set<string>(),
});
const emit = defineEmits<Emits>();

const { canWrite } = usePermission();

/** 来源标签映射 */
const sourceLabels: Record<string, string> = {
  upload: '手动上传',
  certbot_cloudflare_dns: 'Cloudflare DNS',
  certbot_manual_dns: '手动 DNS',
};

/** 剩余天数 badge 颜色 */
function getDaysColor(days: number): 'error' | 'warning' | 'success' {
  if (days <= 0) return 'error';
  if (days <= 15) return 'warning';
  return 'success';
}

/** 剩余天数 badge 文本 */
function getDaysLabel(days: number): string {
  if (days <= 0) return '已过期';
  if (days <= 15) return '即将过期';
  return '正常';
}

const columns = computed<DataTableColumns<Api.Certificate>>(() => {
  const cols: DataTableColumns<Api.Certificate> = [
    {
      title: '名称',
      key: 'name',
      ellipsis: { tooltip: true },
      resizable: true,
      minWidth: 120,
      width: 160,
    },
    {
      title: '域名',
      key: 'domains',
      resizable: true,
      minWidth: 250,
      width: 280,
      render(row) {
        return h(NSpace, { size: 4, wrap: true }, () =>
          row.domains.map((domain) =>
            h(NTag, { size: 'small', bordered: false }, () => domain)
          )
        );
      },
    },
    {
      title: '来源',
      key: 'source',
      width: 120,
      render(row) {
        return sourceLabels[row.source] || row.source;
      },
    },
    {
      title: '到期时间',
      key: 'expire_at',
      width: 170,
      render(row) {
        return formatDateTime(row.expire_at);
      },
    },
    {
      title: '剩余天数',
      key: 'days_remaining',
      width: 110,
      render(row) {
        const days = daysUntil(row.expire_at);
        const color = getDaysColor(days);
        const label = getDaysLabel(days);
        return h(
          NTag,
          { type: color, size: 'small', round: true },
          () => `${days}天 (${label})`
        );
      },
    },
    {
      title: '证书链',
      key: 'chain_valid',
      width: 80,
      align: 'center',
      render(row) {
        if (row.chain_valid) {
          return h(NTooltip, { trigger: 'hover' }, {
            trigger: () => h(NIcon, { color: '#18a058', size: 20 }, () => h(CheckmarkCircleOutline)),
            default: () => '证书链完整',
          });
        }
        return h(NTooltip, { trigger: 'hover' }, {
          trigger: () => h(NIcon, { color: '#d03050', size: 20 }, () => h(AlertCircleOutline)),
          default: () => '证书链不完整',
        });
      },
    },
    {
      title: '自动续期',
      key: 'auto_renew',
      width: 90,
      align: 'center',
      render(row) {
        return h(NSwitch, {
          value: row.auto_renew,
          disabled: true,
          size: 'small',
        });
      },
    },
    {
      title: '关联机器',
      key: 'machine_count',
      width: 90,
      align: 'center',
    },
  ];

  // 写权限时显示操作列（只有删除，后端没有 renew 接口）
  if (canWrite()) {
    cols.push({
      title: '操作',
      key: 'actions',
      width: 80,
      fixed: 'right',
      render(row) {
        const isDeleting = props.deletingIds.has(row.id);
        return h(NSpace, { size: 8 }, () => [
          h(
            NTooltip,
            { trigger: 'hover' },
            {
              trigger: () =>
                h(
                  NButton,
                  {
                    size: 'small',
                    quaternary: true,
                    type: 'error',
                    loading: isDeleting,
                    disabled: isDeleting,
                    onClick: () => emit('delete', row),
                  },
                  {
                    icon: () => h(NIcon, null, () => h(TrashOutline)),
                  }
                ),
              default: () => '删除',
            }
          ),
        ]);
      },
    });
  }

  return cols;
});
</script>

<template>
  <NDataTable
    :columns="columns"
    :data="props.data"
    :loading="props.loading"
    :scroll-x="900"
    :bordered="false"
    size="small"
  />
</template>

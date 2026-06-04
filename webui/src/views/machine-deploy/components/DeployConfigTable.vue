<script setup lang="ts">
import { h } from 'vue';
import type { DataTableColumns } from 'naive-ui';
import { NDataTable, NButton, NSpace } from 'naive-ui';
import StatusBadge from '@/components/common/StatusBadge.vue';
import { formatDateTime } from '@/utils/date';
import { usePermission } from '@/hooks/usePermission';

interface Props {
  data: Api.MachineCertificate[];
  loading: boolean;
  certNameMap?: Record<string, string>;
}

interface Emits {
  (e: 'delete', config: Api.MachineCertificate): void;
  (e: 'deploy', config: Api.MachineCertificate): void;
  (e: 'viewLog', config: Api.MachineCertificate): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const { canWrite } = usePermission();

const columns: DataTableColumns<Api.MachineCertificate> = [
  {
    title: '证书',
    key: 'certificate_id',
    ellipsis: { tooltip: true },
    render(row) {
      const name = props.certNameMap?.[row.certificate_id];
      return name || row.certificate_id;
    },
  },
  {
    title: '证书路径',
    key: 'cert_path',
    ellipsis: { tooltip: true },
  },
  {
    title: '私钥路径',
    key: 'private_key_path',
    ellipsis: { tooltip: true },
  },
  {
    title: '部署后命令',
    key: 'post_deploy_commands',
    ellipsis: { tooltip: true },
    render(row) {
      return row.post_deploy_commands || '-';
    },
  },
  {
    title: '配置版本',
    key: 'config_revision',
    width: 90,
  },
  {
    title: '最后部署状态',
    key: 'last_deploy_status',
    width: 120,
    render(row) {
      if (!row.last_deploy_status) return '-';
      return h(StatusBadge, { status: row.last_deploy_status, type: 'deploy' });
    },
  },
  {
    title: '最后部署时间',
    key: 'last_deploy_at',
    width: 170,
    render(row) {
      return row.last_deploy_at ? formatDateTime(row.last_deploy_at) : '-';
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 220,
    render(row) {
      const buttons = [
        h(
          NButton,
          {
            size: 'small',
            type: 'info',
            quaternary: true,
            onClick: () => emit('viewLog', row),
          },
          { default: () => '日志' }
        ),
      ];

      if (canWrite()) {
        buttons.push(
          h(
            NButton,
            {
              size: 'small',
              type: 'primary',
              quaternary: true,
              onClick: () => emit('deploy', row),
            },
            { default: () => '手动部署' }
          ),
          h(
            NButton,
            {
              size: 'small',
              type: 'error',
              quaternary: true,
              onClick: () => emit('delete', row),
            },
            { default: () => '删除' }
          )
        );
      }

      return h(NSpace, { size: 'small' }, { default: () => buttons });
    },
  },
];
</script>

<template>
  <NDataTable
    :columns="columns"
    :data="data"
    :loading="loading"
    :bordered="false"
    :scroll-x="1000"
  />
</template>

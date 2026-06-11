<script setup lang="ts">
import { ref, reactive, h } from 'vue';
import {
  NCard, NSpace, NButton, NDataTable, NTag, NInput, NIcon, NResult,
  useMessage, useDialog,
} from 'naive-ui';
import { RefreshOutline } from '@vicons/ionicons5';
import type { DataTableColumns } from 'naive-ui';
import UserDialog from './components/UserDialog.vue';
import ConfirmDialog from '@/components/common/ConfirmDialog.vue';
import EmptyState from '@/components/common/EmptyState.vue';
import { useTable } from '@/hooks/useTable';
import { useAuthStore } from '@/store';
import { fetchUsers, updateUserRole, disableUser, resetUserPassword } from '@/service/api/user';
import { formatDateTime } from '@/utils/date';
import { getApiErrorMessage } from '@/utils/error';

const message = useMessage();
const dialog = useDialog();
const authStore = useAuthStore();

// Per-row loading Sets
const disablingIds = reactive(new Set<string>());
const roleChangingIds = reactive(new Set<string>());
const resetPwdIds = reactive(new Set<string>());

// Table
const { data, loading, error, pagination, refresh } = useTable<Api.User>({
  fetchFn: fetchUsers,
  immediate: true,
});

// Create dialog
const showCreateDialog = ref(false);

function handleCreate() {
  showCreateDialog.value = true;
}

function handleCreateSuccess() {
  refresh();
}

// Change role
function handleChangeRole(user: Api.User) {
  const newRole = user.role === 'admin' ? 'user' : 'admin';
  const roleLabel = newRole === 'admin' ? '管理员' : '普通用户';
  dialog.warning({
    title: '修改角色',
    content: `确定要将用户「${user.username}」的角色修改为「${roleLabel}」吗？`,
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      roleChangingIds.add(user.id);
      try {
        await updateUserRole(user.id, newRole);
        message.success('角色修改成功');
        refresh();
      } catch (err: unknown) {
        message.error(getApiErrorMessage(err, '角色修改失败'));
      } finally {
        roleChangingIds.delete(user.id);
      }
    },
  });
}

// Disable user
const showDisableConfirm = ref(false);
const disableLoading = ref(false);
const disablingUser = ref<Api.User | null>(null);

function handleDisableClick(user: Api.User) {
  disablingUser.value = user;
  showDisableConfirm.value = true;
}

async function handleDisableConfirm() {
  if (!disablingUser.value) return;
  const id = disablingUser.value.id;
  disableLoading.value = true;
  disablingIds.add(id);
  try {
    await disableUser(id);
    message.success('用户已禁用');
    showDisableConfirm.value = false;
    refresh();
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '禁用失败'));
  } finally {
    disableLoading.value = false;
    disablingIds.delete(id);
  }
}

// Reset password
const resetPasswordValue = ref('');

function handleResetPassword(user: Api.User) {
  resetPasswordValue.value = '';
  dialog.warning({
    title: '重置密码',
    content: () =>
      h('div', {}, [
        h('p', { class: 'mb-2 text-sm' }, `为用户「${user.username}」设置新密码：`),
        h(NInput, {
          type: 'password',
          showPasswordOn: 'click',
          placeholder: '请输入新密码（至少 6 位）',
          value: resetPasswordValue.value,
          onUpdateValue: (v: string) => {
            resetPasswordValue.value = v;
          },
        }),
      ]),
    positiveText: '确定',
    negativeText: '取消',
    onPositiveClick: async () => {
      if (!resetPasswordValue.value || resetPasswordValue.value.length < 6) {
        message.warning('密码长度至少 6 个字符');
        return false;
      }
      resetPwdIds.add(user.id);
      try {
        await resetUserPassword(user.id, resetPasswordValue.value);
        message.success('密码重置成功');
      } catch (err: unknown) {
        message.error(getApiErrorMessage(err, '密码重置失败'));
      } finally {
        resetPwdIds.delete(user.id);
      }
    },
  });
}

// Table columns
const columns: DataTableColumns<Api.User> = [
  {
    title: '用户名',
    key: 'username',
    width: 150,
    ellipsis: { tooltip: true },
  },
  {
    title: '角色',
    key: 'role',
    width: 120,
    render(row) {
      const typeMap: Record<string, 'success' | 'info'> = {
        admin: 'success',
        user: 'info',
      };
      const labelMap: Record<string, string> = {
        admin: '管理员',
        user: '普通用户',
      };
      return h(NTag, { type: typeMap[row.role] || 'info', size: 'small' }, {
        default: () => labelMap[row.role] || row.role,
      });
    },
  },
  {
    title: '启用状态',
    key: 'enabled',
    width: 100,
    render(row) {
      return h(
        NTag,
        { type: row.enabled ? 'success' : 'error', size: 'small' },
        { default: () => (row.enabled ? '已启用' : '已禁用') },
      );
    },
  },
  {
    title: '创建时间',
    key: 'created_at',
    width: 180,
    render(row) {
      return formatDateTime(row.created_at);
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 260,
    render(row) {
      const isSelf = row.username === authStore.username;
      const isRoleChanging = roleChangingIds.has(row.id);
      const isDisabling = disablingIds.has(row.id);
      const isResettingPwd = resetPwdIds.has(row.id);
      const isRowBusy = isRoleChanging || isDisabling || isResettingPwd;
      return h(NSpace, { size: 'small' }, {
        default: () => [
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'info',
              loading: isRoleChanging,
              disabled: isRowBusy,
              onClick: () => handleChangeRole(row),
            },
            { default: () => '修改角色' },
          ),
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'error',
              loading: isDisabling,
              disabled: isSelf || !row.enabled || isRowBusy,
              onClick: () => handleDisableClick(row),
              title: isSelf ? '不能禁用自己' : undefined,
            },
            { default: () => '禁用' },
          ),
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'warning',
              loading: isResettingPwd,
              disabled: isRowBusy,
              onClick: () => handleResetPassword(row),
            },
            { default: () => '重置密码' },
          ),
        ],
      });
    },
  },
];
</script>

<template>
  <div class="user-page">
    <!-- 操作栏 -->
    <NCard class="mb-4">
      <NSpace justify="space-between" align="center" :wrap="true">
        <span class="text-lg font-medium">用户管理</span>
        <NSpace :wrap="true">
          <NButton type="primary" @click="handleCreate">
            创建用户
          </NButton>
        </NSpace>
      </NSpace>
    </NCard>

    <!-- 错误状态 -->
    <NCard v-if="error && !loading" class="mb-4">
      <NResult status="error" title="加载失败" :description="error">
        <template #footer>
          <NButton type="primary" @click="refresh">
            <template #icon>
              <NIcon><RefreshOutline /></NIcon>
            </template>
            重试
          </NButton>
        </template>
      </NResult>
    </NCard>

    <!-- 数据表格 -->
    <NCard v-else>
      <NDataTable
        :columns="columns"
        :data="data"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: Api.User) => row.id"
        :scroll-x="800"
      />
      <!-- 空状态 -->
      <EmptyState v-if="!loading && data.length === 0" description="暂无用户数据" />
    </NCard>

    <!-- 创建用户对话框 -->
    <UserDialog
      v-model:show="showCreateDialog"
      @success="handleCreateSuccess"
    />

    <!-- 禁用确认对话框 -->
    <ConfirmDialog
      v-model:show="showDisableConfirm"
      title="禁用用户"
      :content="`确定要禁用用户「${disablingUser?.username ?? ''}」吗？禁用后该用户将无法登录，且此操作不可撤销。`"
      confirm-text="禁用"
      type="error"
      :loading="disableLoading"
      @confirm="handleDisableConfirm"
    />
  </div>
</template>

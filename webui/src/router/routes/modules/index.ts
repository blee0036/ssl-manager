import type { RouteRecordRaw } from 'vue-router';

/** 业务路由模块 — 需要认证的页面 */
export const businessRoutes: RouteRecordRaw[] = [
  {
    path: '/dashboard',
    name: 'Dashboard',
    component: () => import('@/views/dashboard/index.vue'),
    meta: {
      title: '仪表盘',
      icon: 'i-carbon-dashboard',
      requiresAuth: true,
      roles: ['admin', 'user', 'readonly'],
      order: 1,
    },
  },
  {
    path: '/certificates',
    name: 'Certificate',
    component: () => import('@/views/certificate/index.vue'),
    meta: {
      title: '证书管理',
      icon: 'i-carbon-certificate',
      requiresAuth: true,
      roles: ['admin', 'user', 'readonly'],
      order: 2,
    },
  },
  {
    path: '/machines',
    name: 'Machine',
    component: () => import('@/views/machine/index.vue'),
    meta: {
      title: '机器管理',
      icon: 'i-carbon-server-rack',
      requiresAuth: true,
      roles: ['admin', 'user', 'readonly'],
      order: 3,
    },
  },
  {
    path: '/machines/:id/deploy',
    name: 'MachineDeploy',
    component: () => import('@/views/machine-deploy/index.vue'),
    meta: {
      title: '机器部署配置',
      icon: 'i-carbon-deploy',
      requiresAuth: true,
      roles: ['admin', 'user', 'readonly'],
      hideInMenu: true,
      order: 4,
    },
  },
  {
    path: '/domains',
    name: 'Domain',
    component: () => import('@/views/domain/index.vue'),
    meta: {
      title: '域名监控',
      icon: 'i-carbon-globe',
      requiresAuth: true,
      roles: ['admin', 'user', 'readonly'],
      order: 5,
    },
  },
  {
    path: '/root-domains',
    name: 'RootDomain',
    component: () => import('@/views/root-domain/index.vue'),
    meta: {
      title: '域名到期监控',
      icon: 'i-carbon-calendar',
      requiresAuth: true,
      roles: ['admin', 'user', 'readonly'],
      order: 5.5,
    },
  },
  {
    path: '/thirdpart-dns',
    name: 'ThirdpartDns',
    component: () => import('@/views/thirdpart-dns/index.vue'),
    meta: {
      title: '第三方 DNS',
      icon: 'i-carbon-dns',
      requiresAuth: true,
      roles: ['admin', 'user'],
      order: 6,
    },
  },
  {
    path: '/alerts',
    name: 'Alert',
    component: () => import('@/views/alert/index.vue'),
    meta: {
      title: '告警管理',
      icon: 'i-carbon-notification',
      requiresAuth: true,
      roles: ['admin', 'user', 'readonly'],
      order: 7,
    },
  },
  {
    path: '/audit-logs',
    name: 'AuditLog',
    component: () => import('@/views/audit-log/index.vue'),
    meta: {
      title: '审计日志',
      icon: 'i-carbon-document',
      requiresAuth: true,
      roles: ['admin', 'user', 'readonly'],
      order: 8,
    },
  },
  {
    path: '/system',
    name: 'System',
    component: () => import('@/views/system/index.vue'),
    meta: {
      title: '系统配置',
      icon: 'i-carbon-settings',
      requiresAuth: true,
      roles: ['admin', 'user'],
      order: 9,
    },
  },
  {
    path: '/users',
    name: 'User',
    component: () => import('@/views/user/index.vue'),
    meta: {
      title: '用户管理',
      icon: 'i-carbon-user-multiple',
      requiresAuth: true,
      roles: ['admin'],
      order: 10,
    },
  },
];

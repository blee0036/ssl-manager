import { createRouter, createWebHistory } from 'vue-router';
import { routes } from './routes';
import { setupRouterGuards } from './guard';

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: () => ({ left: 0, top: 0 }),
});

// Register auth + permission guards
setupRouterGuards(router);

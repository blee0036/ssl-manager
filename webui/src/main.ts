import { createApp } from 'vue';
import { createPinia } from 'pinia';
import App from './App.vue';
import { router } from './router';
import { setupDirectives } from './directives';
import 'virtual:uno.css';
import './styles/mobile.css';

const chunkReloadKey = 'ssl-manager:chunk-reload-at';

function reloadOnceForStaleChunk() {
  const lastReloadAt = Number(sessionStorage.getItem(chunkReloadKey) || '0');
  if (Date.now() - lastReloadAt < 30_000) return;
  sessionStorage.setItem(chunkReloadKey, String(Date.now()));
  window.location.reload();
}

window.addEventListener('vite:preloadError', (event) => {
  event.preventDefault();
  reloadOnceForStaleChunk();
});

router.onError((error) => {
  if (/Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed/i.test(error.message)) {
    reloadOnceForStaleChunk();
  }
});

const app = createApp(App);

app.use(createPinia());
app.use(router);

setupDirectives(app);

app.mount('#app');

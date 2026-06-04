import { createApp } from 'vue';
import { createPinia } from 'pinia';
import App from './App.vue';
import { router } from './router';
import { setupDirectives } from './directives';
import 'virtual:uno.css';
import './styles/mobile.css';

const app = createApp(App);

app.use(createPinia());
app.use(router);

setupDirectives(app);

app.mount('#app');

import { createApp } from 'vue'
import { createPinia } from 'pinia'
import Antd from 'ant-design-vue'
import App from './App.vue'
import { i18n } from './i18n'
import { router } from './router'
import 'ant-design-vue/dist/reset.css'
import './style.css'

createApp(App).use(createPinia()).use(i18n).use(Antd).use(router).mount('#app')

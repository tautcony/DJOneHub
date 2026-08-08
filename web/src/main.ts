import { createApp } from 'vue'
import { createPinia } from 'pinia'
import {
  Alert,
  Button,
  Card,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Layout,
  List,
  Menu,
  Modal,
  Pagination,
  PageHeader,
  Popconfirm,
  Progress,
  Radio,
  Segmented,
  Select,
  Skeleton,
  Space,
  Spin,
  Switch,
  Tabs,
  Tag,
  Tooltip,
} from 'ant-design-vue'
import App from './App.vue'
import { i18n } from './i18n'
import { router } from './router'
import 'ant-design-vue/dist/reset.css'
import './style.css'

// Ant Design 组件按需注册 (替代 app.use(Antd) 全量安装), 缩小首屏体积。
// 注意: 只注册带 install 的父组件 — Radio/Form/Input/Layout/Menu/List/Select
// 的 install 会同时注册其子组件 (a-radio-group、a-form-item、a-input-search
// 等), 子组件本身没有 install, 直接 app.use 会被 Vue 静默跳过, 模板中的
// <a-*> 将渲染为未知元素 (纯文本、不可交互)。新增组件时在此补充父组件。
const antdComponents = [
  Alert,
  Button,
  Card,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Layout,
  List,
  Menu,
  Modal,
  Pagination,
  PageHeader,
  Popconfirm,
  Progress,
  Radio,
  Segmented,
  Select,
  Skeleton,
  Space,
  Spin,
  Switch,
  Tabs,
  Tag,
  Tooltip,
]

const app = createApp(App)
app.use(createPinia())
app.use(i18n)
for (const component of antdComponents) app.use(component)
app.use(router)
app.mount('#app')

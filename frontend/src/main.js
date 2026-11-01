import {createApp} from 'vue'
import MainView from '@/view/MainView.vue'
import {icons} from '@/components/app/icons'
import {busy} from '@/components/app/busy'
import {installControls} from '@/components/app/controls'
import '@/css/theme.css'
import router from '@/router/index.js'

const app = createApp(MainView)
// 引入图标
for (const [key, component] of Object.entries(icons)) {
    app.component(key, component)
}
app.use(router)
installControls(app)
app.directive('busy', busy)
app.mount('#app')

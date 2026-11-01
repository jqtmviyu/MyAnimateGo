import {createRouter, createWebHashHistory} from 'vue-router'
import SubscriptionView from '@/view/home/SubscriptionView.vue'
import LogsView from '@/view/home/LogsView.vue'
import ConfigView from '@/view/home/ConfigView.vue'
import {startupPage} from '@/js/global.js'

const startupPaths = ['/catalog', '/subscriptions', '/logs']

const routes = [
    {
        path: '/',
        redirect: () => startupPaths.includes(startupPage.value) ? startupPage.value : '/catalog'
    },
    {
        path: '/home',
        redirect: '/catalog'
    },
    {
        path: '/catalog',
        component: SubscriptionView
    },
    {
        path: '/subscriptions',
        component: SubscriptionView
    },
    {
        path: '/downloads',
        component: () => import('@/view/downloads/DownloadsView.vue')
    },
    {
        path: '/logs',
        component: LogsView
    },
    {
        path: '/settings',
        component: ConfigView
    }
]

const router = createRouter({
    history: createWebHashHistory(),
    routes
})

export default router

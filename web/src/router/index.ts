import { createRouter, createWebHistory, RouteRecordRaw } from 'vue-router'
import AlertsView from '../pages/AlertsView.vue'
import AlertDetailView from '../pages/AlertDetailView.vue'
import ReadingsView from '../pages/ReadingsView.vue'
import CollectorsView from '../pages/CollectorsView.vue'
import BacklogView from '../pages/BacklogView.vue'

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/alerts' },
  { path: '/alerts', name: 'alerts', component: AlertsView },
  { path: '/alerts/:id', name: 'alert-detail', component: AlertDetailView, props: true },
  { path: '/readings', name: 'readings', component: ReadingsView },
  { path: '/collectors', name: 'collectors', component: CollectorsView },
  { path: '/backlog', name: 'backlog', component: BacklogView }
]

export const router = createRouter({
  history: createWebHistory(),
  routes
})

import { createRouter, createWebHistory } from 'vue-router'
import { getUser } from './api'

const routes = [
  { path: '/login', component: () => import('./views/Login.vue'), meta: { public: true } },
  { path: '/', component: () => import('./views/Home.vue') },
  { path: '/ride', component: () => import('./views/Ride.vue'), meta: { roles: ['user'] } },
  { path: '/dashboard', component: () => import('./views/Dashboard.vue') },
  { path: '/events', component: () => import('./views/Events.vue') },
  { path: '/events/:id', component: () => import('./views/EventDetail.vue') },
  { path: '/rebalance', component: () => import('./views/Rebalance.vue'), meta: { staff: true } },
  { path: '/peak-routes', component: () => import('./views/PeakRoutes.vue'), meta: { staff: true } },
  { path: '/peak-routes/:id', component: () => import('./views/PeakRouteDetail.vue'), meta: { staff: true } },
  { path: '/maintenance', component: () => import('./views/Maintenance.vue'), meta: { staff: true } },
  { path: '/assets', component: () => import('./views/AssetLedger.vue'), meta: { staff: true } },
  { path: '/appeals', component: () => import('./views/Appeals.vue') },
  { path: '/temp-returns', component: () => import('./views/TempReturns.vue'), meta: { staff: true } },
  { path: '/appeals/:id', component: () => import('./views/AppealDetail.vue') },
  { path: '/stations', component: () => import('./views/Stations.vue'), meta: { staff: true } },
  { path: '/ops', component: () => import('./views/Ops.vue'), meta: { staff: true } },
]

const STAFF = ['cs', 'dispatcher', 'repair', 'repair_lead', 'station_admin', 'operator', 'city', 'driver']

const router = createRouter({ history: createWebHistory(), routes })

router.beforeEach((to) => {
  const u = getUser()
  if (to.meta.public) return true
  if (!u) return '/login'
  if (to.meta.roles && !to.meta.roles.includes(u.role)) return '/'
  if (to.meta.staff && !STAFF.includes(u.role)) return '/'
  return true
})

export default router

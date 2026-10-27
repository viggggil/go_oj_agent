import { createRouter, createWebHistory } from 'vue-router'

import { tokens } from './api'
import Login from './views/Login.vue'
import Profile from './views/Profile.vue'
import Register from './views/Register.vue'
import Problems from './views/Problems.vue'
import ProblemDetail from './views/ProblemDetail.vue'
import ProblemEditor from './views/ProblemEditor.vue'
import SubmissionList from './views/SubmissionList.vue'
import SubmissionCreate from './views/SubmissionCreate.vue'
import SubmissionDetail from './views/SubmissionDetail.vue'
import Contests from './views/Contests.vue'
import ContestDetail from './views/ContestDetail.vue'
import ContestLeaderboard from './views/ContestLeaderboard.vue'
import ContestEditor from './views/ContestEditor.vue'
import Home from './views/Home.vue'
import AgentWorkspace from './views/AgentWorkspace.vue'
import AgentChat from './views/AgentChat.vue'
import AgentList from './views/AgentList.vue'
import AgentEditor from './views/AgentEditor.vue'
import AgentRag from './views/AgentRag.vue'
import AgentEval from './views/AgentEval.vue'
import AgentEvalDatasetCreate from './views/AgentEvalDatasetCreate.vue'
import AgentTools from './views/AgentTools.vue'
import MonitoringAgent from './views/MonitoringAgent.vue'
import MonitoringWorkspace from './views/MonitoringWorkspace.vue'
import MonitoringHome from './views/MonitoringHome.vue'
import MonitoringMetrics from './views/MonitoringMetrics.vue'
import JudgePipeline from './views/JudgePipeline.vue'
import { agentAdministrator } from './agent/state'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: Home },
    { path: '/login', component: Login },
    { path: '/register', component: Register },
    { path: '/profile', component: Profile, meta: { auth: true } },
    {
      path: '/monitoring',
      component: MonitoringWorkspace,
      meta: { auth: true },
      children: [
        { path: '', component: MonitoringHome },
        { path: 'metrics', component: MonitoringMetrics, meta: { monitorMetrics: true } },
        { path: 'agent', component: MonitoringAgent, meta: { monitorAgent: true } },
        { path: 'judge', component: JudgePipeline, meta: { monitorSystem: true } },
      ],
    },
    { path: '/problems', component: Problems, meta: { auth: true } },
    { path: '/problems/new', component: ProblemEditor, meta: { auth: true, admin: true } },
    { path: '/problems/:id', component: ProblemDetail, meta: { auth: true } },
    { path: '/problems/:id/edit', component: ProblemEditor, meta: { auth: true, admin: true } },
    { path: '/submissions', component: SubmissionList, meta: { auth: true } },
    { path: '/submissions/new', component: SubmissionCreate, meta: { auth: true } },
    { path: '/submissions/:id', component: SubmissionDetail, meta: { auth: true } },
    { path: '/contests', component: Contests, meta: { auth: true } },
    { path: '/contests/new', component: ContestEditor, meta: { auth: true, admin: true } },
    { path: '/contests/:id', component: ContestDetail, meta: { auth: true } },
    { path: '/contests/:id/leaderboard', component: ContestLeaderboard, meta: { auth: true } },
    {
      path: '/agent',
      component: AgentWorkspace,
      meta: { auth: true },
      children: [
        { path: '', redirect: '/agent/chat' },
        { path: 'chat', component: AgentChat },
        { path: 'rag', component: AgentRag, meta: { agentAdmin: true } },
        { path: 'eval', component: AgentEval, meta: { agentAdmin: true } },
        { path: 'tools', component: AgentTools, meta: { agentAdmin: true } },
        {
          path: 'observability',
          redirect: (to) => ({ path: '/monitoring/agent', query: to.query, hash: to.hash }),
        },
        {
          path: 'eval/datasets/new',
          component: AgentEvalDatasetCreate,
          meta: { agentAdmin: true },
        },
        {
          path: ':resource(agents|prompts|skills)',
          component: AgentList,
          meta: { agentAdmin: true },
        },
        {
          path: ':resource(agents|prompts|skills)/new',
          component: AgentEditor,
          meta: { agentAdmin: true },
        },
        {
          path: ':resource(agents|prompts|skills)/:key/edit',
          component: AgentEditor,
          meta: { agentAdmin: true },
        },
        {
          path: ':resource(agents|prompts|skills)/:key',
          redirect: (to) =>
            '/agent/' + String(to.params.resource) + '/' + String(to.params.key) + '/edit',
          meta: { agentAdmin: true },
        },
      ],
    },
  ],
})

router.beforeEach(async (to, from) => {
  if (to.meta.auth && !tokens.access) {
    return '/login'
  }

  if (to.meta.admin || to.meta.monitorSystem) {
    const { useAuthStore } = await import('./stores/auth')
    const auth = useAuthStore()
    if (!auth.user) await auth.fetchProfile().catch(() => undefined)
    const allowed = to.meta.monitorSystem
      ? auth.user?.roles.some((role) => ['admin', 'system_admin'].includes(role))
      : auth.user?.roles.includes('admin')
    if (!allowed) return '/problems'
  }

  if (to.meta.monitorAgent || to.path.startsWith('/monitoring')) {
    const { useAuthStore } = await import('./stores/auth')
    const auth = useAuthStore()
    if (!auth.user || auth.profileError) await auth.fetchProfile().catch(() => undefined)
    if (!tokens.access) return '/login'
    if (auth.profileError) return false
    if (
      (to.meta.monitorAgent || to.meta.monitorMetrics) &&
      !agentAdministrator(auth.user?.roles || [])
    )
      return '/problems'
    if (to.path === '/monitoring' && !agentAdministrator(auth.user?.roles || [])) return '/problems'
  }

  if (to.path.startsWith('/agent')) {
    const { useAuthStore } = await import('./stores/auth')
    const auth = useAuthStore()
    if (to.path !== from.path || !auth.user || auth.profileError)
      await auth.fetchProfile().catch(() => undefined)
    if (!tokens.access) return '/login'
    if (auth.profileError && from.path.startsWith('/agent')) return false
    if (to.meta.agentAdmin && !auth.profileError && !agentAdministrator(auth.user?.roles || []))
      return '/agent/chat'
  }

  if ((to.path === '/login' || to.path === '/register') && tokens.access) {
    return '/profile'
  }

  return true
})

export default router

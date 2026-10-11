<template>
  <template v-if="resource === 'prompts'">
    <label
      >正文<textarea
        v-model="prompt.text"
        class="agent-prompt"
        required
        maxlength="64000"
        spellcheck="false"
      ></textarea>
    </label>
    <fieldset>
      <legend>模板变量</legend>
      <div class="agent-checks">
        <label v-for="variable in ['language', 'problem_id', 'submission_id']" :key="variable"
          ><input v-model="prompt.variables" type="checkbox" :value="variable" />{{
            variable
          }}</label
        >
      </div>
    </fieldset>
  </template>
  <template v-else>
    <label>名称<input v-model="named.name" required maxlength="128" /></label>
    <fieldset>
      <legend>基础 Prompt</legend>
      <VersionPicker
        label="Prompt"
        resource="prompts"
        :model-value="named.prompt_id ? [named.prompt_id] : []"
        :initial="localBindings"
        @bindings="mergeBindings"
        @update:model-value="named.prompt_id = $event[0] || ''"
        @create="$emit('create', 'prompts')"
      />
    </fieldset>
    <template v-if="resource === 'skills'">
      <label
        >执行模式<select v-model="skill.execution_mode">
          <option value="direct">direct</option>
          <option value="react" disabled>react（不可用）</option>
        </select></label
      >
    </template>
    <template v-else>
      <fieldset>
        <legend>绑定 Skill</legend>
        <VersionPicker
          label="Skill"
          resource="skills"
          multiple
          :model-value="agent.skill_ids"
          :initial="localBindings"
          @bindings="mergeBindings"
          @update:model-value="selectSkills"
          @create="$emit('create', 'skills')"
        />
      </fieldset>
      <label
        >默认 Skill<select v-model="agent.default_skill_id" required>
          <option value="">选择默认 Skill</option>
          <option v-for="id in agent.skill_ids" :key="id" :value="id">
            {{ localBindings.find((item) => item.id === id)?.name || id }}
          </option>
        </select></label
      >
      <fieldset>
        <legend>模型</legend>
        <ModelPicker v-model="agent.model_profile_id" />
      </fieldset>
      <div class="agent-field-grid">
        <label
          >可见范围<select v-model="agent.visibility" :disabled="agent.is_test">
            <option value="user">普通用户</option>
            <option value="admin">管理员</option>
          </select></label
        ><label class="agent-check"
          ><input v-model="agent.is_test" type="checkbox" @change="syncTest" />测试 Agent</label
        ><label v-if="agent.is_test"
          >到期时间（本地时区）<input
            :value="localExpiry"
            type="datetime-local"
            required
            @input="expiryChanged"
        /></label>
      </div>
    </template>
    <fieldset>
      <legend>工具白名单</legend>
      <p class="muted agent-caption">当前运行时尚未执行工具调用；白名单用于保存配置。</p>
      <p v-if="toolError" class="error" role="alert">
        {{ toolError }} <button class="link" type="button" @click="loadTools">重试</button>
      </p>
      <p v-if="!tools.length && !toolError" class="muted">
        {{ toolsLoading ? '加载中...' : '无可用工具' }}
      </p>
      <div class="agent-tool-list">
        <label v-for="tool in tools" :key="tool.name"
          ><input
            v-model="named.allowed_tools"
            type="checkbox"
            :value="tool.name"
            :disabled="
              !named.allowed_tools.includes(tool.name) &&
              (!tool.enabled || tool.side_effect !== 'read')
            "
          /><span
            ><strong>{{ tool.name }}</strong
            ><span class="agent-caption">{{ tool.description }}</span
            ><span class="agent-caption muted"
              >{{ tool.read_scope }} · {{ tool.sensitivity }}</span
            ></span
          >
          <details>
            <summary>Schema</summary>
            <pre>{{
              JSON.stringify({ input: tool.input_schema, output: tool.output_schema }, null, 2)
            }}</pre>
          </details></label
        >
        <div
          v-for="name in named.allowed_tools.filter(
            (value) => !tools.some((tool) => tool.name === value),
          )"
          :key="name"
          class="selected-binding"
        >
          <span>{{ name }} · 未注册</span
          ><button
            class="agent-icon secondary"
            type="button"
            :title="`移除 ${name}`"
            :aria-label="`移除 ${name}`"
            @click="named.allowed_tools = named.allowed_tools.filter((value) => value !== name)"
          >
            <X :size="16" />
          </button>
        </div>
      </div>
    </fieldset>
    <fieldset>
      <legend>运行预算</legend>
      <div class="agent-field-grid">
        <label v-for="[field, label, min, max, step] in budgetFields" :key="field"
          >{{ label
          }}<input
            v-model.number="named.budget[field]"
            type="number"
            :min="min"
            :max="max"
            :step="step"
            required
        /></label>
      </div>
    </fieldset>
  </template>
</template>
<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { X } from '@lucide/vue'
  import VersionPicker from './VersionPicker.vue'
  import ModelPicker from './ModelPicker.vue'
  import { agentApi, agentError } from '../../agent/api'
  import {
    budgetFields,
    type AgentContent,
    type Binding,
    type Content,
    type PromptContent,
    type Resource,
    type SkillContent,
    type Tool,
  } from '../../agent/types'
  const props = defineProps<{ resource: Resource; content: Content; bindings: Binding[] }>()
  defineEmits<{ create: [resource: Resource] }>()
  const localBindings = ref<Binding[]>([])
  function mergeBindings(values: Binding[]) {
    const all = new Map([...localBindings.value, ...values].map((item) => [item.id, item]))
    localBindings.value = [...all.values()]
  }
  watch(() => props.bindings, mergeBindings, { immediate: true, deep: true })
  const prompt = computed(() => props.content as PromptContent),
    skill = computed(() => props.content as SkillContent),
    agent = computed(() => props.content as AgentContent),
    named = computed(() => props.content as SkillContent | AgentContent)
  const tools = ref<Tool[]>([]),
    toolError = ref(''),
    toolsLoading = ref(false),
    controller = new AbortController()
  async function loadTools() {
    toolError.value = ''
    toolsLoading.value = true
    try {
      const values: Tool[] = []
      let page = 1
      while (page <= 501) {
        const { data } = await agentApi.tools(page, controller.signal)
        values.push(...data.items)
        if (page * data.page.page_size >= data.page.total) break
        page++
      }
      if (!controller.signal.aborted) tools.value = values
    } catch (cause) {
      if (!controller.signal.aborted) toolError.value = agentError(cause)
    } finally {
      toolsLoading.value = false
    }
  }
  function syncDefault() {
    if (!agent.value.skill_ids.includes(agent.value.default_skill_id))
      agent.value.default_skill_id = agent.value.skill_ids[0] || ''
  }
  function selectSkills(values: string[]) {
    agent.value.skill_ids = values
    syncDefault()
  }
  function syncTest() {
    if (agent.value.is_test) agent.value.visibility = 'admin'
    else agent.value.test_expires_at = null
  }
  const localExpiry = computed(() => {
    const text = agent.value.test_expires_at
    if (!text) return ''
    const date = new Date(text)
    if (!Number.isFinite(date.getTime())) return ''
    return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
  })
  function expiryChanged(event: Event) {
    const value = (event.target as HTMLInputElement).value
    const date = new Date(value)
    agent.value.test_expires_at =
      value && Number.isFinite(date.getTime()) ? date.toISOString() : null
  }
  onMounted(() => {
    if (props.resource !== 'prompts') loadTools()
  })
  onBeforeUnmount(() => controller.abort())
</script>

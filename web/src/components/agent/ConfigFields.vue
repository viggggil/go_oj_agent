<template>
  <label>名称<input v-model="named.name" required maxlength="128" /></label>
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
    <details>
      <summary>高级设置：模板变量</summary>
      <div class="agent-checks">
        <label v-for="variable in ['language', 'problem_id', 'submission_id']" :key="variable"
          ><input v-model="prompt.variables" type="checkbox" :value="variable" />{{
            variable
          }}</label
        >
      </div>
    </details>
  </template>
  <template v-else>
    <template v-if="resource === 'skills'">
      <label>简短描述<input v-model="skill.description" maxlength="512" /></label>
      <label
        >任务说明<textarea
          v-model="skill.text"
          class="agent-prompt"
          required
          maxlength="64000"
          spellcheck="false"
        ></textarea>
      </label>
      <p class="muted">说明这项任务的目标、回答方式和要求；运行限制继承 Agent。</p>
      <fieldset v-if="skill.variables?.length">
        <legend>从历史 Prompt 保留的模板变量</legend>
        <label v-for="variable in skill.variables" :key="variable">
          <input v-model="skill.variables" type="checkbox" :value="variable" />{{ variable }}
        </label>
      </fieldset>
    </template>
    <template v-else>
      <fieldset>
        <legend>基础 Prompt</legend>
        <VersionPicker
          label="Prompt"
          resource="prompts"
          :model-value="agent.prompt_id ? [agent.prompt_id] : []"
          :initial="localBindings"
          @bindings="mergeBindings"
          @update:model-value="agent.prompt_id = $event[0] || ''"
          @create="$emit('create', 'prompts')"
        />
      </fieldset>
      <fieldset>
        <legend>绑定 Skill</legend>
        <p class="muted">模型会按问题参考已绑定技能的适用说明与任务文本，无需用户选择。</p>
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
      <KnowledgePicker v-model="agent.knowledge_scope" />
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
    <details>
      <summary>高级设置</summary>
      <fieldset>
        <legend>工具白名单</legend>
        <p class="muted agent-caption">
          选择“读取业务信息后回答”时，使用 Agent 与 Skill
          白名单中共同允许的只读工具；业务服务校验用户权限。
        </p>
        <p v-if="toolError" class="error" role="alert">
          {{ toolError }} <button class="link" type="button" @click="loadTools">重试</button>
        </p>
        <p v-if="!tools.length && !toolError" class="muted">
          {{ toolsLoading ? '加载中...' : '无可用工具' }}
        </p>
        <div class="agent-tool-list">
          <label v-for="tool in tools" :key="tool.name"
            ><input
              v-model="toolConfig.allowed_tools"
              type="checkbox"
              :value="tool.name"
              :disabled="
                !toolConfig.allowed_tools.includes(tool.name) &&
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
            v-for="name in toolConfig.allowed_tools.filter(
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
              @click="
                toolConfig.allowed_tools = toolConfig.allowed_tools.filter(
                  (value) => value !== name,
                )
              "
            >
              <X :size="16" />
            </button>
          </div>
        </div>
      </fieldset>
      <fieldset v-if="resource === 'agents'">
        <legend>运行限制</legend>
        <div class="agent-field-grid">
          <label v-for="[field, label, min, max, step] in budgetFields" :key="field"
            >{{ label
            }}<input
              v-model.number="agent.budget[field]"
              type="number"
              :min="min"
              :max="max"
              :step="step"
              required
          /></label>
        </div>
      </fieldset>
    </details>
  </template>
</template>
<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { X } from '@lucide/vue'
  import VersionPicker from './VersionPicker.vue'
  import ModelPicker from './ModelPicker.vue'
  import KnowledgePicker from './KnowledgePicker.vue'
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
    named = computed(() => props.content as SkillContent | AgentContent | PromptContent),
    toolConfig = computed(() => props.content as SkillContent | AgentContent)
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

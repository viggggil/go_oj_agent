<template>
  <article class="trace-tree-node" :class="{ 'trace-tree-node-selected': selected }">
    <div class="trace-tree-row">
      <button class="trace-tree-select" type="button" @click="$emit('select', span.span_id)">
        <span class="trace-tree-title">{{ spanTitle(span) }}</span>
        <span class="trace-tree-duration">{{ span.duration_ms.toFixed(2) }} ms</span>
        <span class="trace-tree-status" :class="statusClass(span)">{{ statusLabel(span) }}</span>
      </button>
      <button
        v-if="children.length"
        class="trace-tree-toggle"
        type="button"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
      >
        {{ expanded ? '收起' : `展开 ${children.length} 个子阶段` }}
      </button>
    </div>
    <dl v-if="selected" class="trace-tree-attributes">
      <div v-for="(value, key) in span.attributes" :key="key">
        <dt>{{ attributeLabel(String(key)) }}</dt>
        <dd>{{ valueLabel(String(key), value) }}</dd>
      </div>
      <div>
        <dt>服务</dt>
        <dd>{{ serviceLabel(span.service_name) }}</dd>
      </div>
      <div>
        <dt>开始时间</dt>
        <dd>{{ formatTime(span.started_at) }}</dd>
      </div>
      <div>
        <dt>Span ID</dt>
        <dd>
          <code>{{ span.span_id }}</code>
        </dd>
      </div>
    </dl>
    <div v-if="children.length && expanded" class="trace-tree-children">
      <TraceSpanNode
        v-for="child in children"
        :key="child.span.span_id"
        :span="child.span"
        :children="child.children"
        :selected-span-id="selectedSpanId"
        @select="$emit('select', $event)"
      />
    </div>
  </article>
</template>

<script setup lang="ts">
  import { computed, ref } from 'vue'
  import type { TraceSpan } from '../../api'

  export type TraceSpanTreeNode = { span: TraceSpan; children: TraceSpanTreeNode[] }
  const props = defineProps<{
    span: TraceSpan
    children: TraceSpanTreeNode[]
    selectedSpanId: string
  }>()
  defineEmits<{ select: [spanId: string] }>()

  function hasBusinessDescendant(nodes: TraceSpanTreeNode[]): boolean {
    return nodes.some(
      (node) => node.span.name.startsWith('agent.') || hasBusinessDescendant(node.children),
    )
  }
  const expanded = ref(
    ['agent.run', 'agent.context.load', 'agent.step', 'agent.model.attempt', 'agent.tool'].includes(
      props.span.name,
    ) || hasBusinessDescendant(props.children),
  )
  const selected = computed(() => props.selectedSpanId === props.span.span_id)

  function spanTitle(span: TraceSpan) {
    const attrs = span.attributes
    const step = attrs['agent.step']
    const phase = attrs['agent.phase']
    if (span.name === 'agent.step') {
      if (phase === 'reason') return `第 ${step || '?'} 轮推理`
      if (phase === 'tool') return `第 ${step || '?'} 次工具执行`
      if (phase === 'finalize') return '整理最终回答'
    }
    if (span.name === 'agent.tool' && typeof attrs['agent.tool'] === 'string') {
      return `工具：${attrs['agent.tool']}`
    }
    return (
      {
        'gateway.http': 'Gateway 对话请求',
        'HTTP POST': 'Gateway 到 Agent 请求',
        'POST /api/v1/agent/chat': 'Agent 对话接口',
        'agent.run': 'Agent 一次运行',
        'agent.context.load': '加载会话上下文',
        'agent.model.attempt': '模型调用',
        'agent.tool': 'Agent 工具调用',
        'agent.rag.retrieve': '知识检索',
        'agent.grpc.client': '业务服务调用',
        'agent.finalize': '保存最终回答',
        'judge.submission.create': '创建判题提交',
        'judge.submission.rejudge': '重新提交判题',
        'judge.outbox.publish': '发布判题任务',
        'judge.worker.task': 'Worker 执行判题',
        'judge.worker.result.publish': '发布判题结果',
        'judge.worker.retry.publish': '发布判题重试',
        'judge.result.consume': '消费判题结果',
        'judge.result.apply': '应用判题结果',
        'judge.result.transaction': '保存判题结果',
        'problem.v1.ProblemService/GetJudgeProfile': '读取判题配置',
      }[span.name] || span.name
    )
  }
  function statusLabel(span: TraceSpan) {
    const value = span.attributes['agent.status']
    if (value === 'failed' || span.status === 'STATUS_CODE_ERROR') return '失败'
    if (value === 'cancelled') return '已取消'
    if (value === 'running') return '进行中'
    if (
      value === 'completed' ||
      span.status === 'STATUS_CODE_UNSET' ||
      span.status === 'STATUS_CODE_OK'
    )
      return '完成'
    return span.status
  }
  function statusClass(span: TraceSpan) {
    const label = statusLabel(span)
    return label === '失败'
      ? 'trace-status-failed'
      : label === '进行中'
        ? 'trace-status-running'
        : ''
  }
  function serviceLabel(value: string) {
    return (
      {
        'gateway-service': 'Gateway',
        'agent-service': 'Agent 服务',
        'agent-rag-worker': 'Agent RAG Worker',
        'judge-service': '判题服务',
        'judge-worker': '判题 Worker',
        'contest-service': '比赛服务',
        'problem-service': '题目服务',
      }[value] || value
    )
  }
  function attributeLabel(key: string) {
    return (
      {
        'agent.run_id': 'Run ID',
        'agent.purpose': '运行用途',
        'agent.status': '运行状态',
        'agent.error_code': '错误码',
        'agent.model': '模型',
        'agent.tool': '工具',
        'agent.attempt': '调用序号',
        'agent.step': '步骤序号',
        'agent.phase': '阶段',
        'agent.result_count': '检索结果数',
        'agent.history_turn_count': '历史轮数',
        'agent.history_summary_used': '使用会话摘要',
        'agent.input_tokens': '输入 Token',
        'agent.output_tokens': '输出 Token',
        'agent.usage_source': '用量来源',
        'agent.truncated': '结果已截断',
        'judge.operation': '判题操作',
        'judge.attempt': '判题尝试次数',
        'judge.revision': '判题版本',
        'submission.id': '提交 ID',
        'submission.status': '提交状态',
        'submission.language': '提交语言',
        'submission.contest': '比赛提交',
        'messaging.event_type': '消息事件类型',
        'messaging.routing_key': '消息路由键',
        'messaging.causation_id': '因果事件 ID',
        'outbox.event_id': 'Outbox 事件 ID',
        'error.code': '错误码',
        'rpc.system': 'RPC 类型',
        'rpc.method': 'RPC 方法',
        'rpc.grpc.status_code': 'RPC 状态码',
      }[key] || key
    )
  }
  function valueLabel(key: string, value: string | number | boolean) {
    if (key === 'agent.purpose') return value === 'chat' ? '对话' : value
    if (key === 'agent.status') {
      return (
        { completed: '完成', failed: '失败', cancelled: '已取消', running: '进行中' }[
          String(value)
        ] || value
      )
    }
    if (key === 'agent.phase') {
      return (
        { reason: '推理', tool: '工具执行', observe: '整理工具结果', finalize: '生成最终回答' }[
          String(value)
        ] || value
      )
    }
    if (key === 'agent.history_summary_used') return value ? '是' : '否'
    if (key === 'agent.truncated') return value ? '是' : '否'
    return value
  }
  function formatTime(value: string) {
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '未知' : date.toLocaleString()
  }
</script>

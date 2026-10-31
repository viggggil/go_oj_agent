import { test, expect } from '@playwright/test'

async function authenticate(page: import('@playwright/test').Page, role = 'user') {
  let createdProblem: unknown
  let submittedSource: unknown
  await page.addInitScript(() => {
    localStorage.setItem('access_token', 'problem-test-access')
    localStorage.setItem('refresh_token', 'problem-test-refresh')
  })
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/v1/users/me') {
      return route.fulfill({
        json: {
          user: {
            id: 4,
            username: 'problem-admin',
            email: 'problem@example.test',
            status: 'active',
            roles: [role],
          },
        },
      })
    }
    if (path === '/api/v1/problems' && route.request().method() === 'GET') {
      return route.fulfill({ json: { items: [{ id: 42, title: 'A+B' }], page: { total: 1 } } })
    }
    if (path === '/api/v1/problems/42' && route.request().method() === 'GET') {
      return route.fulfill({
        json: {
          problem: {
            id: 42,
            title: 'A+B',
            slug: 'a-plus-b',
            description:
              '# A+B\n\n## 题目描述\n将两个整数相加。\n\n## 数据范围\n旧范围\n\n## 样例输入\n```text\n旧输入\n```\n\n## 样例输出\n```text\n旧输出\n```\n\n## 来源\n公开来源\n\n<img src=x onerror=alert(1)>',
            constraints: '1 ≤ a,b ≤ 100',
            samples: [{ input: '2 3\n', output: '5\n' }],
            difficulty: 1,
            time_limit_ms: 1000,
            memory_limit_kb: 65536,
            status: 1,
            tags: [],
          },
        },
      })
    }
    if (path === '/api/v1/problems/42/testcases') return route.fulfill({ json: { items: [] } })
    if (path === '/api/v1/problems' && route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      createdProblem = body.problem
      return route.fulfill({ json: { problem: { id: 42, ...body.problem, status: 1, tags: [] } } })
    }
    if (path === '/api/v1/submissions' && route.request().method() === 'POST') {
      submittedSource = route.request().postDataJSON()
      return route.fulfill({ json: { submission_id: 9, status: 'SUBMISSION_STATUS_QUEUED' } })
    }
    return route.fulfill({ status: 404, json: {} })
  })
  return {
    createdProblem: () => createdProblem,
    submittedSource: () => submittedSource,
  }
}

test('管理员可创建带样例和数据范围的题目，最多三组并保留换行', async ({ page }) => {
  const captured = await authenticate(page, 'admin')
  await page.goto('/problems/new')
  await page.getByLabel('题目名称').fill('A+B')
  await page.getByLabel('Slug').fill('a-plus-b')
  await page.getByLabel('题面描述').fill('将两个整数相加。')
  await page.getByLabel('数据范围').fill('1 ≤ a,b ≤ 100')
  await page.locator('.sample-editor-row').nth(0).getByLabel('输入').fill('2 3\n')
  await page.locator('.sample-editor-row').nth(0).getByLabel('输出').fill('5\n')
  await page.getByRole('button', { name: '添加样例' }).click()
  await page.locator('.sample-editor-row').nth(1).getByLabel('输入').fill('7 -2')
  await page.locator('.sample-editor-row').nth(1).getByLabel('输出').fill('5')
  await page.getByRole('button', { name: '添加样例' }).click()
  await page.locator('.sample-editor-row').nth(2).getByLabel('输入').fill('0 0')
  await page.locator('.sample-editor-row').nth(2).getByLabel('输出').fill('0')
  await expect(page.getByRole('button', { name: '添加样例' })).toHaveCount(0)
  await page.getByRole('button', { name: '保存题目' }).click()
  await expect(page).toHaveURL(/\/problems\/42\/edit$/)
  await expect(page.locator('.sample-editor-row')).toHaveCount(3)
  await expect(page.locator('.sample-editor-row').nth(0).getByLabel('输入')).toHaveValue('2 3\n')
  expect(captured.createdProblem()).toMatchObject({
    constraints: '1 ≤ a,b ≤ 100',
    samples: [
      { input: '2 3\n', output: '5\n' },
      { input: '7 -2', output: '5' },
      { input: '0 0', output: '0' },
    ],
  })
})

test('详情页用结构化样例替代旧题面章节，提交页可选择五种语言', async ({ page }) => {
  const captured = await authenticate(page)
  await page.goto('/problems/42')
  await expect(page.getByText('将两个整数相加。')).toBeVisible()
  await expect(page.getByRole('heading', { name: '数据范围' })).toHaveCount(1)
  await expect(page.getByText('旧范围')).toHaveCount(0)
  await expect(page.getByText('旧输入')).toHaveCount(0)
  await expect(page.getByText('旧输出')).toHaveCount(0)
  await expect(page.locator('.problem-description img')).toHaveCount(0)
  await expect(page.getByText('2 3', { exact: true })).toBeVisible()
  await expect(page.getByText('5', { exact: true })).toBeVisible()

  await page.goto('/submissions/new?problem_id=42')
  const language = page.getByLabel('语言')
  for (const [value, extension] of [
    ['go', '.go'],
    ['c', '.c'],
    ['cpp', '.cpp'],
    ['java', '.java'],
    ['python', '.py'],
  ]) {
    await language.selectOption(value)
    await expect(page.locator('input[type=file]')).toHaveAttribute('accept', new RegExp(extension))
  }
  await language.selectOption('python')
  await page.getByLabel('代码').fill('print(input())')
  await page.getByRole('button', { name: '提交代码' }).click()
  await expect(page).toHaveURL(/\/submissions\/9$/)
  expect(captured.submittedSource()).toMatchObject({
    problem_id: 42,
    language: 'python',
    source_code: 'print(input())',
  })
})

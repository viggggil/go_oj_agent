"""隔离集成测试的 Responses 服务；不调用外部网络。"""

import asyncio
import json

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, StreamingResponse

app = FastAPI()
completed = asyncio.Event()
cancelled = asyncio.Event()


def frame(kind, **values):
    return 'event: ' + kind + '\ndata: ' + json.dumps({'type': kind, **values}, ensure_ascii=False) + '\n\n'


@app.get('/healthz')
async def health():
    return {'status': 'ok'}


@app.get('/completed')
async def status():
    return {'completed': completed.is_set()}


@app.get('/cancelled')
async def cancellation_status():
    return {'cancelled': cancelled.is_set()}


@app.post('/release')
async def release():
    completed.set()
    return {'status': 'released'}


@app.post('/v1/responses')
async def responses(request: Request):
    body = await request.json()
    if request.headers.get('authorization') != 'Bearer integration-model-key':
        return JSONResponse({'error': 'unauthorized'}, status_code=401)
    if body.get('store') is not False or body.get('model') != 'deepseek-v4-flash':
        return JSONResponse({'error': 'invalid config'}, status_code=400)
    completed.clear()
    last = body['input'][-1]
    message = last.get('content', '')
    if message in {'PR3 read problem', 'PR3 denied problem'} or last.get('type') == 'function_call_output':
        if body.get('parallel_tool_calls') is not False or [tool['name'] for tool in body.get('tools', [])] != ['get_problem']:
            return JSONResponse({'error': 'wrong tools'}, status_code=400)
        if last.get('type') == 'function_call_output':
            if last.get('call_id') != 'call_read_problem' or body['input'][-2].get('name') != 'get_problem':
                return JSONResponse({'error': 'wrong tool output binding'}, status_code=400)
            result = json.loads(last['output'])
            text = '已读取题目，证据数字 739216' if result.get('ok') and '739216' in last['output'] else '无权读取题目'
            if not result.get('ok') and result.get('error_code') != 'TOOL_PERMISSION_DENIED':
                return JSONResponse({'error': 'unexpected tool failure'}, status_code=400)
            async def answer():
                yield frame('response.output_text.delta', delta=text, output_index=0)
                yield frame('response.completed', response={'status': 'completed', 'output': [
                    {'type': 'message', 'role': 'assistant', 'content': [{'type': 'output_text', 'text': text}]}]})
            return StreamingResponse(answer(), media_type='text/event-stream')
        async def call():
            yield frame('response.function_call_arguments.delta', delta='{"problem_id":', output_index=0)
            yield frame('response.completed', response={'status': 'completed', 'output': [
                {'type': 'function_call', 'name': 'get_problem', 'call_id': 'call_read_problem',
                 'arguments': json.dumps({'problem_id': 42 if message == 'PR3 denied problem' else 41}), 'status': 'completed'}]})
        return StreamingResponse(call(), media_type='text/event-stream')
    if message in {'PR2 context followup', 'PR2 browser followup'}:
        first = 'PR2 remember 314159' if message == 'PR2 context followup' else 'PR9 browser original'
        expected = [
            {'role': 'user', 'content': first},
            {'role': 'assistant', 'content': '中文算法回答'},
            {'role': 'user', 'content': message},
        ]
        if body['input'] != expected:
            return JSONResponse({'error': 'wrong completed history'}, status_code=400)
    if message == 'PR2 new conversation' and body['input'] != [{'role': 'user', 'content': message}]:
        return JSONResponse({'error': 'history leaked to new conversation'}, status_code=400)
    if message == 'wait-for-cancel':
        cancelled.clear()
    if message == 'PR7 normal chat' and not all(
        value in body.get('instructions', '')
        for value in ['PR7 测试基础提示', 'PR7 测试 Skill 提示']
    ):
        return JSONResponse({'error': 'wrong prompt binding'}, status_code=400)
    if message == 'PR9 browser original' and 'PR8 浏览器真实配置提示' not in body.get('instructions', ''):
        return JSONResponse({'error': 'wrong original binding'}, status_code=400)
    if message == 'PR9 browser rebound' and ('新提示' not in body.get('instructions', '') or 'PR8 浏览器真实配置提示' in body.get('instructions', '')):
        return JSONResponse({'error': 'wrong rebound binding'}, status_code=400)
    async def events():
        try:
            yield frame('response.output_text.delta', delta='中文算法回答', output_index=0)
            if message in {'wait-for-release', 'wait-for-cancel'}:
                await completed.wait()
            else:
                completed.set()
            if message == 'fail-after-token':
                yield frame('response.failed', response={'status': 'failed', 'error': {'message': 'private-api-key'}})
                return
            yield frame('response.completed', response={
                'status': 'completed', 'model': body['model'],
                'output': [{'type': 'message', 'role': 'assistant', 'content': [
                    {'type': 'output_text', 'text': '中文算法回答'}]}],
                'usage': {'input_tokens': 300, 'output_tokens': 8}
            })
        finally:
            if message == 'wait-for-cancel':
                cancelled.set()
    return StreamingResponse(events(), media_type='text/event-stream')

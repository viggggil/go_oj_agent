"""隔离集成测试的 Responses 服务；不调用外部网络。"""

import asyncio
import json

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, StreamingResponse

app = FastAPI()
completed = asyncio.Event()


def frame(kind, **values):
    return 'event: ' + kind + '\ndata: ' + json.dumps({'type': kind, **values}, ensure_ascii=False) + '\n\n'


@app.get('/healthz')
async def health():
    return {'status': 'ok'}


@app.get('/completed')
async def status():
    return {'completed': completed.is_set()}


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
    message = body['input'][0]['content']
    if message == 'PR7 normal chat' and not all(
        value in body.get('instructions', '')
        for value in ['PR7 测试基础提示', 'PR7 测试 Skill 提示']
    ):
        return JSONResponse({'error': 'wrong prompt binding'}, status_code=400)
    async def events():
        yield frame('response.output_text.delta', delta='中文算法回答', output_index=0)
        if message == 'wait-for-release':
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
    return StreamingResponse(events(), media_type='text/event-stream')

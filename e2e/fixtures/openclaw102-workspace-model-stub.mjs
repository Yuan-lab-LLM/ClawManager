// Deterministic protocol fixture; no external model or credentials.
import http from 'node:http';
const calls = [];
http.createServer((req, res) => {
  res.setHeader('content-type', 'application/json');
  if (req.method === 'GET') return res.end(JSON.stringify(req.url === '/stats' ? { calls } :
    { object: 'list', data: [{ id: 'local-smoke' }, { id: 'local-second' }] }));
  if (req.url !== '/v1/chat/completions') { res.statusCode = 404; return res.end('{}'); }
  let raw = '';
  req.on('data', chunk => raw += chunk);
  req.on('end', () => {
    const body = JSON.parse(raw);
    calls.push({ model: body.model, stream: body.stream === true });
    const content = 'LOCAL_WORKSPACE_REPLY';
    const base = { id: `local-${calls.length}`, created: 1700000000, model: body.model };
    if (body.stream) {
      res.setHeader('content-type', 'text/event-stream');
      for (const [delta, finish] of [[{ role: 'assistant', content }, null], [{}, 'stop']])
        res.write('data: ' + JSON.stringify({ ...base, object: 'chat.completion.chunk', choices: [{ index: 0, delta, finish_reason: finish }] }) + '\n\n');
      res.end('data: [DONE]\n\n');
    } else res.end(JSON.stringify({ ...base, object: 'chat.completion', choices: [{ index: 0, message: { role: 'assistant', content }, finish_reason: 'stop' }], usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 } }));
  });
}).listen(19334, '0.0.0.0', () => console.log('LOCAL_WORKSPACE_STUB_READY'));

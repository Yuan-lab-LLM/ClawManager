import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createNames, validCreateNames, parseCreateEnvironment, defaultCreateImage, maskCreateEnvironment } from '../src/lib/instanceCreation.ts';

test('environment accepts blank/comment lines, empty values and equals inside values', () => {
  const parsed = parseCreateEnvironment('# ignored\r\nA=left=right\r\n\r\nEMPTY=\nSPACES=  retained  ');
  assert.equal(parsed.error, undefined);
  assert.deepEqual({ ...parsed.values }, { A: 'left=right', EMPTY: '', SPACES: '  retained  ' });
});
test('environment rejects invalid/duplicate names and uses a safe own-property map', () => {
  assert.deepEqual(parseCreateEnvironment('A=1\nA=2').error, {line:2, duplicate:'A'});
  assert.deepEqual(parseCreateEnvironment('1A=value').error, {line:1});
  const parsed = parseCreateEnvironment('__proto__=data\nconstructor=literal');
  assert.equal(Object.getPrototypeOf(parsed.values), null);
  assert.equal(parsed.values.__proto__, 'data');
});
test('name generation and bounds match the server batch contract', () => {
  assert.deepEqual(createNames(' agent ', true, 3, 7), ['agent-007', 'agent-008', 'agent-009']);
  assert.deepEqual(createNames('agent', false, 100, 7), ['agent']);
  for (const args of [[1,1],[101,1],[2.5,1],[2,0],[2,10000],[2,NaN]]) assert.deepEqual(createNames('agent',true,...args),[]);
  assert.equal(validCreateNames(createNames('x'.repeat(40),true,2,1)),true);
  assert.equal(validCreateNames(createNames('x'.repeat(41),true,2,1)),false);
  assert.equal(validCreateNames(['中'.repeat(17)]),false);
});
test('image choice only uses enabled settings for the requested type and mode', () => {
  const settings=[{instance_type:'openclaw',runtime_type:'gateway',is_enabled:false,image:'disabled'},{instance_type:'openclaw',runtime_type:'desktop',image:'pro'},{instance_type:'hermes',runtime_type:'gateway',image:'other'},{instance_type:'openclaw',runtime_type:'gateway',image:'lite'}];
  assert.equal(defaultCreateImage(settings,'openclaw','lite').image,'lite');
  assert.equal(defaultCreateImage(settings,'openclaw','pro').image,'pro');
  assert.equal(defaultCreateImage(settings,'opencode','lite'),undefined);
  assert.equal(defaultCreateImage([{instance_type:'custom',runtime_type:'gateway',image:'unsupported'}],'custom','lite'),undefined);
});
test('secret preview never reveals sensitive values', () => {
  for(const key of ['API_TOKEN','OPENAI_API_KEY','PRIVATE_KEY','password','AUTHORIZATION']) assert.equal(maskCreateEnvironment(key,'secret'),'••••••••');
  assert.equal(maskCreateEnvironment('LANG','zh_CN.UTF-8'),'zh_CN.UTF-8');
});

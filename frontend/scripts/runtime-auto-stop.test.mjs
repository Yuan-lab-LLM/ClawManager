import assert from 'node:assert/strict';
import test from 'node:test';
import { parseAutoStopDays } from '../src/lib/runtimeAutoStop.ts';

test('automatic stop accepts independent positive whole-day values within the duration range', () => {
  for (const value of ['1', '2', '7', '106751']) assert.equal(parseAutoStopDays(value), Number(value));
});

test('automatic stop rejects empty, noninteger, nonpositive and overflowing day inputs', () => {
  for (const value of ['', ' ', '0', '-1', '1.5', '1.0', '1e2', 'Infinity', '106752', '9007199254740993']) {
    assert.equal(parseAutoStopDays(value), undefined, value);
  }
});

test('automatic stop converts each selected unit to exact seconds and round-trips equivalent values', async () => {
 const { parseAutoStopSeconds, autoStopDuration, MAX_AUTO_STOP_SECONDS } = await import('../src/lib/runtimeAutoStop.ts');
 assert.equal(typeof parseAutoStopSeconds, 'function', 'seconds settings are not implemented');
 for (const [unit, seconds] of [['second', 1], ['minute', 60], ['hour', 3600], ['day', 86400]]) {
  assert.equal(parseAutoStopSeconds('7', unit), 7 * seconds);
  const display = autoStopDuration(7 * seconds);
  assert.equal(parseAutoStopSeconds(display.value, display.unit), 7 * seconds);
 }
 assert.deepEqual(autoStopDuration(61), { value: '61', unit: 'second' });
 for (const value of ['', '0', '-1', '1.5', '1e2', String(MAX_AUTO_STOP_SECONDS + 1)]) assert.equal(parseAutoStopSeconds(value, 'second'), undefined);
 assert.equal(parseAutoStopSeconds('106752', 'day'), undefined);
 assert.equal(parseAutoStopSeconds('1', 'week'), undefined);
});

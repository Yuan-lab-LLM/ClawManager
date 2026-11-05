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
